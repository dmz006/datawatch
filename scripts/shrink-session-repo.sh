#!/usr/bin/env bash
# shrink-session-repo.sh — operator-invoked remediation for session-tracker
# repos bloated by pre-fix tracker.go committing output.log (BL420).
#
# Deletes output.log / output.log.enc from git HISTORY of each given session
# repo. The working-tree output.log is disposable live-tail data from history's
# point of view, but the on-disk file itself is left byte-for-byte untouched.
#
# Why this is a separate script: on 2026-10-13 an in-daemon `git repack` of one
# of these repos hit ~108GB RSS and the OOM killer took down datawatch.service.
# The heavy git work here therefore runs in its own memory-capped transient
# systemd scope (outside the daemon's cgroup), so an overrun kills only that
# scope. Without systemd-run --user it falls back to nice/ionice + ulimit -v.
#
# Usage:
#   scripts/shrink-session-repo.sh [--dry-run] [--allow-active] <session-repo-dir>...
#   scripts/shrink-session-repo.sh [--dry-run] --all-hygiene <sessions-parent-dir>...
#
#   --dry-run       print what would happen; change nothing
#   --allow-active  rewrite even if the session looks active (index.lock is
#                   still re-checked immediately before rewriting)
#   --all-hygiene   given parent dir(s) (e.g. ~/.datawatch/sessions), apply ONLY
#                   the cheap hygiene config + info/exclude to every */.git
#                   session repo — no history rewrite, non-destructive
#
# Environment:
#   SHRINK_MEMORY_MAX   memory cap for the heavy git steps (default 4G)
#   SHRINK_NO_SCOPE=1   skip systemd-run and use the nice/ionice/ulimit fallback
#   SHRINK_FILTER_TOOL  force "filter-repo" or "filter-branch" (default: filter-repo
#                       when installed, else filter-branch)
#   SHRINK_ALLOW_DAEMON_CGROUP=1  allow the fallback to run inside datawatch.service
#
# Exit 0 only if every repo succeeded (or, in dry-run, was inspected).
set -euo pipefail

ts() { echo "[$(date '+%H:%M:%S')] $*"; }
warn() { echo "[$(date '+%H:%M:%S')] WARN: $*" >&2; }

SELF="$(readlink -f "${BASH_SOURCE[0]}")"
MEM_MAX="${SHRINK_MEMORY_MAX:-4G}"
OUTPUT_FILES=(output.log output.log.enc)
# Same list as outputLogExcludes in internal/session/tracker.go.
EXCLUDES=(output.log output.log.enc output.log.pipe '*.pipe')
# Same values as ensureRepoHygiene in internal/session/tracker.go, plus a
# bounded delta cache for the explicit gc this script runs.
HYGIENE=(
	"gc.auto=0"
	"gc.autoDetach=false"
	"pack.windowMemory=64m"
	"pack.threads=1"
	"pack.deltaCacheSize=32m"
	"core.bigFileThreshold=16m"
)

# mem_to_kib "4G" -> 4194304
mem_to_kib() {
	local v="$1" n unit
	n="${v%[KkMmGgTt]}"
	unit="${v#"$n"}"
	[[ "$n" =~ ^[0-9]+$ ]] || { echo "invalid SHRINK_MEMORY_MAX: $v" >&2; return 1; }
	case "$unit" in
	"") echo $((n / 1024)) ;;
	[Kk]) echo "$n" ;;
	[Mm]) echo $((n * 1024)) ;;
	[Gg]) echo $((n * 1024 * 1024)) ;;
	[Tt]) echo $((n * 1024 * 1024 * 1024)) ;;
	esac
}

human_kib() { awk -v k="$1" 'BEGIN{ if (k>=1048576) printf "%.2fGiB", k/1048576; else printf "%.1fMiB", k/1024 }'; }

# ─── worker: the heavy git steps, run inside the memory-capped scope ─────────
#
# Runs from inside <repo>/.git with core.bare=true so that neither
# filter-branch (read-tree -u) nor filter-repo (reset --hard) touches the
# working tree — they both skip their checkout step for bare repos.
worker() {
	local tool="$1" repo="$2"
	cd "$repo/.git"
	local -a G=(git -c core.bare=true
		-c pack.windowMemory=64m -c pack.threads=1 -c pack.deltaCacheSize=32m
		-c core.bigFileThreshold=16m
		-c core.packedGitLimit=1g -c core.packedGitWindowSize=64m)

	# Background RSS sampler over this worker's process tree (1s cadence).
	local peakfile
	peakfile="$(mktemp)"
	echo 0 >"$peakfile"
	local me=$$
	(
		max=0
		while kill -0 "$me" 2>/dev/null; do
			cur=$(ps -eo pid=,ppid=,rss= | awk -v root="$me" '
				{ pp[$1]=$2; rss[$1]=$3 }
				END {
					tot=0
					for (p in pp) { q=p; while (q!="" && q!=0 && q!=1) { if (q==root) { tot+=rss[p]; break } q=pp[q] } }
					print tot
				}')
			if ((cur > max)); then max=$cur; echo "$max" >"$peakfile"; fi
			sleep 1
		done
	) &
	local sampler=$!

	ts "  [worker pid $$] cgroup: $(cut -d: -f3 /proc/self/cgroup)"
	ts "  [worker] rewriting history with $tool"
	case "$tool" in
	filter-repo)
		"${G[@]}" filter-repo --invert-paths --path output.log --path output.log.enc --force --no-gc
		;;
	filter-branch)
		rm -rf "$PWD/shrink-filter-branch.tmp" # left behind if a previous run was killed
		FILTER_BRANCH_SQUELCH_WARNING=1 "${G[@]}" filter-branch -f -d "$PWD/shrink-filter-branch.tmp" \
			--index-filter 'git rm -q --cached --ignore-unmatch output.log output.log.enc' -- --all 2>&1 |
			tr '\r' '\n' | awk '!/^Rewrite [0-9a-f]+ \(/ && NF'
		;;
	esac

	ts "  [worker] dropping refs/original, expiring reflogs"
	"${G[@]}" for-each-ref --format='%(refname)' refs/original/ | while read -r ref; do
		"${G[@]}" update-ref -d "$ref"
	done
	rm -rf refs/original
	"${G[@]}" reflog expire --expire=now --expire-unreachable=now --all

	ts "  [worker] git gc --prune=now (not --aggressive)"
	"${G[@]}" gc --prune=now --quiet

	ts "  [worker] git fsck --no-progress"
	"${G[@]}" fsck --no-progress

	kill "$sampler" 2>/dev/null || true
	wait "$sampler" 2>/dev/null || true
	local sampled cg_peak="" cgpath
	sampled="$(cat "$peakfile")"
	rm -f "$peakfile"
	cgpath="/sys/fs/cgroup$(cut -d: -f3 /proc/self/cgroup)/memory.peak"
	if [[ "${SHRINK_IN_SCOPE:-0}" == 1 && -r "$cgpath" ]]; then
		cg_peak="$(cat "$cgpath")"
		ts "  [worker] PEAK MEMORY: scope memory.peak=$(human_kib $((cg_peak / 1024))) (cap $MEM_MAX); sampled RSS max=$(human_kib "$sampled")"
	else
		ts "  [worker] PEAK MEMORY: sampled RSS max=$(human_kib "$sampled") (cap $MEM_MAX via ulimit -v)"
	fi
}

if [[ "${1:-}" == "--_worker" ]]; then
	shift
	worker "$@"
	exit 0
fi

# ─── argument parsing ────────────────────────────────────────────────────────
DRY_RUN=0
ALL_HYGIENE=0
ALLOW_ACTIVE=0
TARGETS=()
while [[ $# -gt 0 ]]; do
	case "$1" in
	--dry-run) DRY_RUN=1 ;;
	--all-hygiene) ALL_HYGIENE=1 ;;
	--allow-active) ALLOW_ACTIVE=1 ;;
	-h | --help)
		sed -n '2,/^set -euo/p' "$SELF" | sed '$d; s/^# \{0,1\}//'
		exit 0
		;;
	--*) echo "unknown flag: $1" >&2; exit 2 ;;
	*) TARGETS+=("$1") ;;
	esac
	shift
done
if [[ ${#TARGETS[@]} -eq 0 ]]; then
	echo "usage: $0 [--dry-run] [--all-hygiene] [--allow-active] <session-repo-dir>..." >&2
	exit 2
fi
MEM_KIB="$(mem_to_kib "$MEM_MAX")"

# ─── helpers ─────────────────────────────────────────────────────────────────
is_session_dir() { [[ -f "$1/session.json" || -f "$1/timeline.md" ]]; }

session_state() {
	local f="$1/session.json"
	[[ -f "$f" ]] || return 0
	if command -v jq >/dev/null 2>&1; then
		jq -r '.state // empty' "$f" 2>/dev/null || true
	else
		grep -o '"state"[[:space:]]*:[[:space:]]*"[^"]*"' "$f" | head -1 | sed 's/.*"\([^"]*\)"$/\1/'
	fi
}

ensure_hygiene() {
	local repo="$1" kv key val excl line
	for kv in "${HYGIENE[@]}"; do
		key="${kv%%=*}"
		val="${kv#*=}"
		if [[ "$(git -C "$repo" config --local --get "$key" 2>/dev/null || true)" != "$val" ]]; then
			if ((DRY_RUN)); then
				echo "    would set $key=$val"
			else
				git -C "$repo" config --local "$key" "$val"
			fi
		fi
	done
	excl="$repo/.git/info/exclude"
	for line in "${EXCLUDES[@]}"; do
		if ! { [[ -f "$excl" ]] && grep -qxF -- "$line" "$excl"; }; then
			if ((DRY_RUN)); then
				echo "    would add '$line' to .git/info/exclude"
			else
				mkdir -p "$(dirname "$excl")"
				if [[ -s "$excl" && -n "$(tail -c1 "$excl")" ]]; then echo >>"$excl"; fi
				printf '%s\n' "$line" >>"$excl"
			fi
		fi
	done
}

scope_available() {
	[[ "${SHRINK_NO_SCOPE:-0}" != 1 ]] && command -v systemd-run >/dev/null 2>&1 &&
		systemd-run --user --scope --quiet -p MemoryMax="$MEM_MAX" true >/dev/null 2>&1
}

pick_tool() {
	case "${SHRINK_FILTER_TOOL:-}" in
	filter-repo | filter-branch) echo "$SHRINK_FILTER_TOOL" ;;
	"") if git filter-repo --version >/dev/null 2>&1; then echo filter-repo; else echo filter-branch; fi ;;
	*) echo "invalid SHRINK_FILTER_TOOL: $SHRINK_FILTER_TOOL" >&2; return 1 ;;
	esac
}

print_stats() {
	local repo="$1" label="$2"
	ts "  $label: .git size $(du -sh "$repo/.git" | cut -f1), commits $(git -C "$repo" rev-list --all --count 2>/dev/null || echo 0)"
	git -C "$repo" count-objects -vH | sed 's/^/      /'
}

# Restore state for the trap: parallel arrays of backup -> original path.
RESTORE_FROM=()
RESTORE_TO=()
restore_backups() {
	local i
	for i in "${!RESTORE_FROM[@]}"; do
		local from="${RESTORE_FROM[$i]}" to="${RESTORE_TO[$i]}"
		[[ -e "$from" ]] || continue
		if [[ -e "$to" ]]; then
			warn "$to was recreated during the rewrite; leaving the original at $from — merge manually"
			continue
		fi
		mv -- "$from" "$to"
	done
	RESTORE_FROM=()
	RESTORE_TO=()
}

# ─── --all-hygiene sweep ─────────────────────────────────────────────────────
if ((ALL_HYGIENE)); then
	rc=0
	for parent in "${TARGETS[@]}"; do
		[[ -d "$parent" ]] || { warn "$parent: not a directory"; rc=1; continue; }
		ts "hygiene sweep: $parent$( ((DRY_RUN)) && echo ' (dry-run)')"
		n=0 skipped=0
		for d in "$parent"/*/; do
			d="${d%/}"
			[[ -d "$d/.git" ]] || continue
			if ! is_session_dir "$d"; then
				echo "  skip (not a session dir): $d"
				skipped=$((skipped + 1))
				continue
			fi
			echo "  $d"
			ensure_hygiene "$d"
			n=$((n + 1))
		done
		ts "hygiene sweep done: $n repo(s) $( ((DRY_RUN)) && echo 'inspected' || echo 'configured'), $skipped skipped"
	done
	exit "$rc"
fi

# ─── per-repo shrink ─────────────────────────────────────────────────────────
USE_SCOPE=0
if scope_available; then USE_SCOPE=1; fi
TOOL="$(pick_tool)"

shrink_one() {
	local repo top
	repo="$(cd "$1" 2>/dev/null && pwd -P)" || { warn "$1: no such directory"; return 1; }
	ts "=== $repo"

	# Validate.
	top="$(git -C "$repo" rev-parse --show-toplevel 2>/dev/null || true)"
	if [[ "$top" != "$repo" ]]; then warn "$repo: not the top of a git repo"; return 1; fi
	if ! is_session_dir "$repo"; then warn "$repo: no session.json or timeline.md — not a datawatch session dir"; return 1; fi
	if [[ -d "$repo/.git/shrink-backup" ]] && [[ -n "$(ls -A "$repo/.git/shrink-backup")" ]]; then
		warn "$repo: leftover $repo/.git/shrink-backup from an interrupted run — move its files back to the repo root first"
		return 1
	fi
	if ! git -C "$repo" rev-parse -q --verify HEAD >/dev/null; then
		ts "  empty repo (no commits) — nothing to do"
		return 0
	fi

	print_stats "$repo" "before"

	# Safety gates.
	if [[ -e "$repo/.git/index.lock" ]]; then warn "$repo: .git/index.lock exists — a git process is running; refusing"; return 1; fi
	local state last age active=""
	state="$(session_state "$repo")"
	last="$(git -C "$repo" log -1 --format=%ct HEAD)"
	age=$(($(date +%s) - last))
	case "$state" in running | waiting_input) active="session.json state=$state" ;; esac
	if ((age < 120)); then active="${active:+$active; }last commit ${age}s ago"; fi
	if [[ -n "$active" ]]; then
		if ((ALLOW_ACTIVE)); then
			warn "session looks active ($active) — proceeding because --allow-active"
		else
			warn "$repo: session looks active ($active); refusing (use --allow-active to override)"
			return 1
		fi
	fi

	# Is there anything in history to remove?
	local hist
	hist="$(git -C "$repo" rev-list --all --count -- "${OUTPUT_FILES[@]}")"
	local tl_before cv_before
	tl_before="$(git -C "$repo" rev-list --all --count -- timeline.md)"
	cv_before="$(git -C "$repo" rev-list --all --count -- conversation.md)"
	ts "  history: $hist commit(s) touch output.log*; timeline.md=$tl_before conversation.md=$cv_before"

	if ((DRY_RUN)); then
		ts "  [dry-run] hygiene config/exclude:"
		ensure_hygiene "$repo"
		if ((hist == 0)); then
			ts "  [dry-run] output.log not in history — would only apply hygiene"
		else
			ts "  [dry-run] would move ${OUTPUT_FILES[*]} aside to .git/shrink-backup/, rewrite history with $TOOL,"
			ts "  [dry-run] drop refs/original, expire reflogs, gc --prune=now, fsck — $(
				((USE_SCOPE)) && echo "in a systemd --user scope (MemoryMax=$MEM_MAX, MemorySwapMax=0)" ||
					echo "under nice/ionice + ulimit -v (no systemd --user scope)")"
		fi
		return 0
	fi

	ts "  applying hygiene config + info/exclude"
	ensure_hygiene "$repo"
	if ((hist == 0)); then
		ts "  output.log not in history — nothing to rewrite"
		return 0
	fi

	# Last-moment lock check, then move the live files out of the working tree.
	if [[ -e "$repo/.git/index.lock" ]]; then warn "$repo: .git/index.lock appeared; refusing"; return 1; fi
	local bdir="$repo/.git/shrink-backup" f
	local -A size_before=() inode_before=()
	mkdir -p "$bdir"
	for f in "${OUTPUT_FILES[@]}"; do
		[[ -e "$repo/$f" ]] || continue
		size_before[$f]="$(stat -c %s "$repo/$f")"
		inode_before[$f]="$(stat -c %i "$repo/$f")"
		RESTORE_FROM+=("$bdir/$f")
		RESTORE_TO+=("$repo/$f")
		mv -- "$repo/$f" "$bdir/$f"
		ts "  moved $f (${size_before[$f]} bytes) aside to .git/shrink-backup/ (restore: mv '$bdir/$f' '$repo/$f')"
	done
	# Untrack before gc: an index entry would keep the newest output.log blob alive.
	git -C "$repo" rm -q -f --cached --ignore-unmatch -- "${OUTPUT_FILES[@]}"

	local rc=0
	if ((USE_SCOPE)); then
		ts "  running $TOOL in systemd --user scope: MemoryMax=$MEM_MAX MemorySwapMax=0 nice 19 ionice idle"
		SHRINK_IN_SCOPE=1 systemd-run --user --scope --quiet -p MemoryMax="$MEM_MAX" -p MemorySwapMax=0 \
			nice -n 19 ionice -c3 "$SELF" --_worker "$TOOL" "$repo" || rc=$?
	else
		if grep -q 'datawatch\.service' /proc/self/cgroup && [[ "${SHRINK_ALLOW_DAEMON_CGROUP:-0}" != 1 ]]; then
			warn "no systemd --user scope available and this shell is inside datawatch.service's cgroup; refusing"
			restore_backups
			return 1
		fi
		local vlimit=$((MEM_KIB * 2))
		ts "  systemd-run --user unavailable — fallback: nice 19, ionice idle, ulimit -v $(human_kib "$vlimit")"
		local -a io=()
		if ionice -c3 true 2>/dev/null; then io=(ionice -c3); fi
		(ulimit -v "$vlimit" && exec nice -n 19 "${io[@]}" "$SELF" --_worker "$TOOL" "$repo") || rc=$?
	fi

	restore_backups
	rmdir "$bdir" 2>/dev/null || true
	if ((rc != 0)); then
		if ((rc == 137 || rc == 143)); then
			warn "$repo: worker was killed by signal (exit $rc) — almost certainly hit the $MEM_MAX cap; only the scope died. Retry with a larger SHRINK_MEMORY_MAX."
		else
			warn "$repo: heavy git step failed (exit $rc)"
		fi
		return 1
	fi

	# Verify.
	local ok=1
	for f in "${!size_before[@]}"; do
		local sz ino
		sz="$(stat -c %s "$repo/$f" 2>/dev/null || echo missing)"
		ino="$(stat -c %i "$repo/$f" 2>/dev/null || echo missing)"
		if [[ "$ino" != "${inode_before[$f]}" ]]; then
			warn "$f: inode changed (${inode_before[$f]} -> $ino)"; ok=0
		elif [[ "$sz" == "${size_before[$f]}" ]]; then
			ts "  verify: working-tree $f untouched ($sz bytes, same inode)"
		elif ((ALLOW_ACTIVE)) && [[ "$sz" -gt "${size_before[$f]}" ]]; then
			ts "  verify: working-tree $f grew ${size_before[$f]} -> $sz bytes (live session appended; same inode)"
		else
			warn "$f: size changed ${size_before[$f]} -> $sz"; ok=0
		fi
	done
	if [[ -n "$(git -C "$repo" log --all --format=%H -- "${OUTPUT_FILES[@]}")" ]]; then
		warn "output.log still present in history"; ok=0
	else
		ts "  verify: git log --all -- output.log is empty"
	fi
	local tl_after cv_after
	tl_after="$(git -C "$repo" rev-list --all --count -- timeline.md)"
	cv_after="$(git -C "$repo" rev-list --all --count -- conversation.md)"
	if [[ "$tl_after" == "$tl_before" && "$cv_after" == "$cv_before" ]]; then
		ts "  verify: timeline.md=$tl_after conversation.md=$cv_after commits (unchanged)"
	else
		warn "history mismatch: timeline.md $tl_before -> $tl_after, conversation.md $cv_before -> $cv_after"; ok=0
	fi
	print_stats "$repo" "after"
	((ok)) || return 1
	ts "  OK"
}

ts "shrink-session-repo: tool=$TOOL, $( ((USE_SCOPE)) && echo "systemd --user scope MemoryMax=$MEM_MAX" || echo "fallback ulimit/nice/ionice")$( ((DRY_RUN)) && echo ', DRY RUN')"
failed=0
for t in "${TARGETS[@]}"; do
	# Subshell (not `shrink_one || ...`, which would disable set -e inside it)
	# with its own trap so moved-aside files are restored on any exit.
	set +e
	(
		set -e
		trap restore_backups EXIT
		trap 'exit 130' INT TERM
		shrink_one "$t"
	)
	rc=$?
	set -e
	((rc == 0)) || failed=$((failed + 1))
done
if ((failed)); then
	ts "done: $failed of ${#TARGETS[@]} repo(s) FAILED or refused"
	exit 1
fi
ts "done: ${#TARGETS[@]} repo(s) OK"
