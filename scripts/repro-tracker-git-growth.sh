#!/usr/bin/env bash
# repro-tracker-git-growth.sh — bounded-scale reproduction of the 2026-10-13
# tracker OOM mechanism (BL420).
#
# Pre-fix, internal/session/tracker.go committed output.log on every state
# change with git's default gc settings. Each commit stored a fresh full blob
# of the ever-growing log, and git's implicit `gc --auto` eventually repacked
# all of them inside the daemon's cgroup. This script simulates both the
# LEGACY and FIXED tracker repo setups in a disposable scratch directory and
# proves the fix by reproduction rather than by reading code.
#
# Usage:
#   scripts/repro-tracker-git-growth.sh --workdir <dir> [--commits N] [--chunk-kb K] [--go]
#
#   --workdir   scratch directory (created if missing; refused under ~/.datawatch)
#   --commits   commits per mode (default 200)
#   --chunk-kb  KB of random base64 appended to output.log per commit (default 512)
#   --go        also run `go test -run TestTracker_OutputLogNotCommitted ./internal/session/`
#   --keep      keep the legacy/ and fixed/ repos after measuring (default: removed,
#               since legacy grows quadratically — ~8GB at the defaults)
#
# Note: git's auto-gc trigger samples only .git/objects/17/, so whether it fires
# in LEGACY mode at bounded scale is probabilistic; it is reported, not required.
#
# Exit 0 with "RESULT: PASS" only when legacy reproduces the growth and the
# fixed setup does not; otherwise "RESULT: FAIL: <reason>" and exit 1.
set -euo pipefail

ts() { echo "[$(date '+%H:%M:%S')] $*"; }
die() { echo "RESULT: FAIL: $*"; exit 1; }

WORKDIR=""
COMMITS=200
CHUNK_KB=512
RUN_GO=0
KEEP=0

while [[ $# -gt 0 ]]; do
	case "$1" in
	--workdir) WORKDIR="${2:-}"; shift 2 ;;
	--commits) COMMITS="${2:-}"; shift 2 ;;
	--chunk-kb) CHUNK_KB="${2:-}"; shift 2 ;;
	--go) RUN_GO=1; shift ;;
	--keep) KEEP=1; shift ;;
	-h | --help) sed -n '2,26p' "$0"; exit 0 ;;
	*) echo "unknown argument: $1" >&2; exit 2 ;;
	esac
done

[[ -n "$WORKDIR" ]] || { echo "--workdir is required" >&2; exit 2; }
[[ "$COMMITS" =~ ^[1-9][0-9]*$ ]] || { echo "--commits must be a positive integer" >&2; exit 2; }
[[ "$CHUNK_KB" =~ ^[1-9][0-9]*$ ]] || { echo "--chunk-kb must be a positive integer" >&2; exit 2; }

mkdir -p "$WORKDIR"
WORKDIR="$(cd "$WORKDIR" && pwd -P)"
DW_HOME="$HOME/.datawatch"
if [[ -d "$DW_HOME" ]]; then
	DW_HOME="$(cd "$DW_HOME" && pwd -P)"
fi
case "$WORKDIR/" in
"$DW_HOME"/*) echo "refusing to run under $DW_HOME — use a disposable scratch dir" >&2; exit 2 ;;
esac

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd -P)"

# Resolve GNU time by command name via $PATH. In bash `command -v time` names
# the shell keyword, so ask a POSIX sh (no `time` keyword) to resolve it.
TIME_BIN="$(sh -c 'command -v time' 2>/dev/null || true)"
if [[ "$TIME_BIN" != /* ]] || ! "$TIME_BIN" -f '%M' -o /dev/null true 2>/dev/null; then
	TIME_BIN=""
fi
if [[ -n "$TIME_BIN" ]]; then
	ts "RSS measurement: GNU time (-f %M)"
else
	ts "RSS measurement: ps sampling (GNU time not on PATH)"
fi

# Isolate from the operator's git config so results are reproducible.
export GIT_CONFIG_NOSYSTEM=1
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_TERMINAL_PROMPT=0

# measure_commit <rssfile> — runs `git add -A && git commit` (tracker commitAll)
# and appends the max RSS (KB) observed to <rssfile>.
measure_commit() {
	local rssfile="$1" msg="$2" out
	if [[ -n "$TIME_BIN" ]]; then
		out="$(mktemp "$WORKDIR/.rss.XXXXXX")"
		"$TIME_BIN" -f '%M' -o "$out" sh -c 'git add -A && git commit -q -m "$1"' _ "$msg"
		tail -n 1 "$out" >>"$rssfile"
		rm -f "$out"
	else
		local pid peak=0 cur
		sh -c 'git add -A && git commit -q -m "$1"' _ "$msg" &
		pid=$!
		while kill -0 "$pid" 2>/dev/null; do
			# sum RSS of the sh wrapper and its git children
			cur="$(ps -o rss= --ppid "$pid" -p "$pid" 2>/dev/null | awk '{s+=$1} END {print s+0}')"
			((cur > peak)) && peak=$cur
			sleep 0.02
		done
		wait "$pid"
		echo "$peak" >>"$rssfile"
	fi
}

# run_mode <legacy|fixed> — prints "size_kb max_rss_kb gc(y/n) log_commits loose packs log_bytes"
run_mode() {
	local mode="$1" dir="$WORKDIR/$1"
	rm -rf "$dir"
	mkdir -p "$dir"
	(
		cd "$dir"
		git init -q -b main
		git config user.email "datawatch@localhost"
		git config user.name "datawatch"
		if [[ "$mode" == legacy ]]; then
			# git defaults, except a small gc.auto so auto-gc fires at bounded scale.
			git config gc.auto 50
		else
			git config gc.auto 0
			git config gc.autoDetach false
			git config pack.windowMemory 64m
			git config pack.threads 1
			git config core.bigFileThreshold 16m
			printf '%s\n' output.log output.log.enc output.log.pipe >>.git/info/exclude
		fi
		local trace="$WORKDIR/$mode.trace2.json" rssfile="$WORKDIR/$mode.rss"
		: >"$trace"
		: >"$rssfile"
		export GIT_TRACE2_EVENT="$trace"
		local bytes=$((CHUNK_KB * 1024 * 3 / 4)) i
		for ((i = 1; i <= COMMITS; i++)); do
			head -c "$bytes" /dev/urandom | base64 >>output.log
			echo "- $(date '+%H:%M:%S') commit $i" >>timeline.md
			measure_commit "$rssfile" "session: state $i"
			if ((i % 25 == 0)); then ts "  $mode: $i/$COMMITS commits, .git $(du -sk .git | cut -f1) KB" >&2; fi
		done
		unset GIT_TRACE2_EVENT
		# A detached auto-gc may still be running; wait (bounded) before measuring.
		local w
		for ((w = 0; w < 600; w++)); do [[ -e .git/gc.pid ]] || break; sleep 0.1; done

		local size rss loose packs logc gc
		size="$(du -sk .git | cut -f1)"
		rss="$(sort -n "$rssfile" | tail -n 1)"
		loose="$(git count-objects -v | awk '/^count:/ {print $2}')"
		packs="$(git count-objects -v | awk '/^packs:/ {print $2}')"
		logc="$(git log --oneline -- output.log | wc -l | tr -d ' ')"
		gc=n
		if ((packs > 0)) || grep -qE '"argv":\[[^]]*"(gc|repack|pack-objects)"' "$trace"; then
			gc=y
		fi
		echo "$size $rss $gc $logc $loose $packs $(wc -c <output.log | tr -d ' ')"
	)
	((KEEP)) || rm -rf "$dir"
}

T0=$(date +%s)
ts "workdir=$WORKDIR commits=$COMMITS chunk_kb=$CHUNK_KB"
ts "legacy worst case before any gc: ~$((CHUNK_KB * COMMITS * (COMMITS + 1) / 2 / 1024)) MB of loose blobs in $WORKDIR/legacy"
ts "LEGACY mode start"
read -r L_SIZE L_RSS L_GC L_LOGC L_LOOSE L_PACKS L_LOGB < <(run_mode legacy)
ts "LEGACY mode done"
ts "FIXED mode start"
read -r F_SIZE F_RSS F_GC F_LOGC F_LOOSE F_PACKS F_LOGB < <(run_mode fixed)
ts "FIXED mode done"

GO_RESULT="skipped"
if ((RUN_GO)); then
	ts "go test TestTracker_OutputLogNotCommitted start"
	if (cd "$REPO_ROOT" && go test -count=1 -run TestTracker_OutputLogNotCommitted ./internal/session/); then
		GO_RESULT="pass"
	else
		GO_RESULT="fail"
	fi
	ts "go test done: $GO_RESULT"
fi

echo
printf '%-7s | %12s | %18s | %8s | %16s | %6s | %5s | %14s\n' \
	mode ".git size KB" "max commit RSS KB" "auto-gc" "output.log commits" loose packs "output.log KB"
printf '%s\n' "--------+--------------+--------------------+----------+------------------+--------+-------+---------------"
printf '%-7s | %12s | %18s | %8s | %18s | %6s | %5s | %14s\n' \
	legacy "$L_SIZE" "$L_RSS" "$L_GC" "$L_LOGC" "$L_LOOSE" "$L_PACKS" "$((L_LOGB / 1024))"
printf '%-7s | %12s | %18s | %8s | %18s | %6s | %5s | %14s\n' \
	fixed "$F_SIZE" "$F_RSS" "$F_GC" "$F_LOGC" "$F_LOOSE" "$F_PACKS" "$((F_LOGB / 1024))"
echo
echo "go test: $GO_RESULT   elapsed: $(($(date +%s) - T0))s"

# Legacy must actually exhibit the problem: every commit captured output.log and
# .git grew in proportion to the total log volume (base64 of random bytes only
# compresses to ~75%, so at least half the final log size must be in .git).
((L_LOGC == COMMITS)) || die "legacy did not reproduce: output.log commits $L_LOGC != $COMMITS"
((L_SIZE * 2 >= L_LOGB / 1024)) || die "legacy .git (${L_SIZE}KB) did not grow with output.log ($((L_LOGB / 1024))KB)"
((F_LOGC == 0)) || die "fixed mode committed output.log $F_LOGC times"
[[ "$F_GC" == n ]] || die "auto-gc ran in fixed mode"
((F_SIZE * 4 < L_SIZE)) || die "fixed .git (${F_SIZE}KB) is not < 1/4 of legacy (${L_SIZE}KB)"
[[ "$GO_RESULT" != fail ]] || die "go test TestTracker_OutputLogNotCommitted failed"
echo "RESULT: PASS"
