#!/usr/bin/env bash
# cleanup-live-check.sh — live check of the Automaton worktree cleanup on a
# SANDBOX daemon, against a throwaway project repository.
#
# What it proves (each result is a numbered check in the report):
#   - report mode: GET /api/autonomous/cleanup counts the removable Automaton
#     branches as would-remove, and removes nothing;
#   - on mode: only the safe entries go (finished, work already on the
#     project's own default branch, clean work tree, past retention); a dirty
#     work tree, an unmerged branch, an unfinished Automaton, a young entry
#     and a branch outside the Automata namespace all stay; each removal is in
#     the Automaton's triage record and in the audit log;
#   - release at end: cancelling an Automaton calls the same release;
#   - temp files: a worker session's scratch file is kept while the session is
#     live and gone once it ends, and a prefixed name the daemon never created
#     is never touched;
#   - the REST, MCP, CLI and comm-channel reads of the status agree.
#
# Generality: nothing here names this repository. The project is a throwaway
# repository created inside the sandbox whose default branch is NOT "main"
# (it is the remote's own default, found by git from refs/remotes/origin/HEAD);
# the worktree base repository and the worktree root are handed to the daemon
# through its normal configuration keys (autonomous.worktree_base_repo,
# autonomous.worktree_dir); the Automata branch namespace is one constant below.
#
# Safety: SANDBOX only. The daemon runs from a generated config under a
# throwaway directory on loopback ports, with its own data directory, its own
# TMPDIR and its own tmux socket (a PATH shim pins every tmux call it makes to
# a socket owned by this script). The production daemon, its service and its
# data are never touched. The daemon is ended only by the PID that
# scripts/test-daemon-lib.sh recorded, with the config path as marker; the
# private tmux server is ended through its own explicit socket. Everything this
# script made is removed on every exit path.
#
# Usage (always from a shell with the operator's tmux variables removed):
#   env -u TMUX -u TMUX_TMPDIR bash scripts/cleanup-live-check.sh
#
# Env:
#   CLEANUP_CHECK_REPORT   where the tracked report is written
#                          (default: docs/plans/2026-10-30-automation-cleanup-live-check.md)
#   TEST_BINARY            daemon binary (default: bin/datawatch, then PATH)
#
# Exit 0 only when every check passed.
#
# The report is regenerated on every run. Its "Findings and mismatches" section
# is written by hand after a run and is not produced by this script.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/scripts/test-daemon-lib.sh"
REPORT="${CLEANUP_CHECK_REPORT:-$ROOT/docs/plans/2026-10-30-automation-cleanup-live-check.md}"

# The Automata branch namespace — defined once, used everywhere below.
AUTOMATON_PREFIX="automaton/"
# The throwaway project's default branch: deliberately not "main".
PROJECT_DEFAULT_BRANCH="trunk"
# Pseudo-user branch, outside the Automata namespace.
USER_BRANCH="feature/user-owned"

unset TMUX TMUX_TMPDIR

# --- scratch area and lifecycle ----------------------------------------------
S="$(mktemp -d /tmp/dw-cleanup-live.XXXXXX)"
export TD_STATE_DIR="$S/tdstate"
REPORT_BUF="$(mktemp /tmp/dw-cleanup-live-report.XXXXXX)"
# shellcheck source=/dev/null
source "$LIB"

REAL_TMUX="$(command -v tmux 2>/dev/null || true)"
TMUX_SOCK="$S/tmux.sock"

check_cleanup_extra() {
  # Runs AFTER td_stop_all (the daemon is gone): end the private tmux server
  # through its explicit throwaway socket, then remove the scratch area.
  if [[ -n "$REAL_TMUX" && -S "$TMUX_SOCK" ]]; then
    "$REAL_TMUX" -S "$TMUX_SOCK" kill-server >/dev/null 2>&1 || true
  fi
  # Worktrees are plain directories of the throwaway repo; removing the area
  # removes them together with the repositories.
  rm -rf "$S" 2>/dev/null || true
  rm -f "$REPORT_BUF" 2>/dev/null || true
}
TD_EXTRA_CLEANUP=check_cleanup_extra
td_install_traps "$@"

# --- result bookkeeping --------------------------------------------------------
PASS=0
FAIL=0
RESULTS=()
CHECK_NO=0
say() { printf '%s\n' "$*"; }
rep() { printf '%s\n' "$*" >> "$REPORT_BUF"; }
rep_block() { # rep_block <label> <text>: fenced observed output, bounded
  rep ""
  rep "$1"
  rep '```'
  printf '%s\n' "$2" | head -c 4000 >> "$REPORT_BUF"
  printf '\n' >> "$REPORT_BUF"
  rep '```'
}
verdict() { # verdict <PASS|FAIL> <description> [evidence]
  local v="$1" desc="$2" ev="${3:-}"
  CHECK_NO=$((CHECK_NO + 1))
  if [[ "$v" == PASS ]]; then PASS=$((PASS + 1)); else FAIL=$((FAIL + 1)); fi
  RESULTS+=("$CHECK_NO|$v|$desc")
  say "  $v  [$CHECK_NO] $desc"
  rep ""
  rep "**Check $CHECK_NO — $v:** $desc"
  if [[ -n "$ev" ]]; then rep ""; rep "$ev"; fi
}
section() { say ""; say "== $* =="; rep ""; rep "## $*"; }
now() { date '+%H:%M:%S'; }

# --- prerequisites ---------------------------------------------------------
for t in git curl python3 jq; do
  command -v "$t" >/dev/null 2>&1 || { say "SKIP: $t is not on PATH"; exit 0; }
done
[[ -n "$REAL_TMUX" ]] || { say "SKIP: tmux is not on PATH"; exit 0; }

BINARY=""
if [[ -n "${TEST_BINARY:-}" && -x "${TEST_BINARY:-}" ]]; then
  BINARY="$TEST_BINARY"
elif [[ -x "$ROOT/bin/datawatch" ]]; then
  BINARY="$ROOT/bin/datawatch"
else
  BINARY="$(command -v datawatch 2>/dev/null || true)"
fi
[[ -n "$BINARY" ]] || { say "SKIP: no datawatch binary (TEST_BINARY, bin/datawatch, PATH)"; exit 0; }
VERSION="$("$BINARY" version 2>/dev/null | head -1 || true)"

free_port() {
  python3 -I -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}
PORT="$(free_port)"; TLS_PORT="$(free_port)"; MCP_PORT="$(free_port)"
DNS_PORT="$(free_port)"; HOOK_PORT="$(free_port)"; NTFY_PORT="$(free_port)"
TOKEN="dw-test-token-12345"
BASE="https://127.0.0.1:$TLS_PORT"

REMOTE="$S/remote.git"      # the project's "origin"
REPO="$S/project"           # the worktree base repository (autonomous.worktree_base_repo)
WTROOT="$S/worktrees"       # the worktree root (autonomous.worktree_dir)
DATA="$S/data"
PROJ="$S/projects"          # allowed project root for the two worker sessions
DTMP="$S/daemon-tmp"        # the daemon's own TMPDIR: the temp sweep scans this
CFG="$S/config.yaml"
LOG="$S/daemon.log"
mkdir -p "$WTROOT" "$DATA" "$PROJ" "$DTMP" "$S/bin"

api() { # api <METHOD> <path> [json body] — body to stdout
  local m="$1" p="$2" b="${3:-}"
  if [[ -n "$b" ]]; then
    curl -sk -m 30 -X "$m" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$b" "$BASE$p"
  else
    curl -sk -m 30 -X "$m" -H "Authorization: Bearer $TOKEN" "$BASE$p"
  fi
}
api_code() { # api_code <METHOD> <path> [json body] — HTTP status only
  local m="$1" p="$2" b="${3:-}"
  curl -sk -m 30 -o /dev/null -w '%{http_code}' -X "$m" -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' ${b:+-d "$b"} "$BASE$p"
}
jq_r() { jq -r "$@" 2>/dev/null; }
dw() { env -u TMUX -u TMUX_TMPDIR "$BINARY" --config "$CFG" "$@"; }

wait_until() { # wait_until <timeout-seconds> <description> <command...>
  local t="$1"; shift; shift
  local end=$((SECONDS + t))
  while ((SECONDS < end)); do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 2
  done
  return 1
}

gitr() { git -C "$REPO" "$@"; }

# --- the throwaway project -------------------------------------------------
# id|name|status|age-hours|kind  (kind: merged | dirty | unmerged | nowt)
CASES=(
  "c0a10001|release-completed|completed|72|merged"
  "c0a10002|release-cancelled|cancelled|72|merged"
  "c0a10003|release-branch-only|completed|72|nowt"
  "c0a10004|held-dirty|completed|72|dirty"
  "c0a10005|held-unmerged|completed|72|unmerged"
  "c0a10006|held-unfinished|paused|72|merged"
  "c0a10007|held-young|completed|0|merged"
  "c0a10008|end-release|paused|72|merged"
)
branch_of() { printf '%s%s-%s' "$AUTOMATON_PREFIX" "$1" "$2"; }
wt_of() { printf '%s/%s-%s' "$WTROOT" "$1" "$2"; }
case_field() { # case_field <id> <n>   (1-based field of the CASES row)
  local row
  for row in "${CASES[@]}"; do
    if [[ "${row%%|*}" == "$1" ]]; then printf '%s' "$row" | cut -d'|' -f"$2"; return 0; fi
  done
}

build_project() {
  git init -q --bare -b "$PROJECT_DEFAULT_BRANCH" "$REMOTE"
  git clone -q "$REMOTE" "$REPO" 2>/dev/null
  gitr config user.email "cleanup-live-check@example.invalid"
  gitr config user.name "cleanup live check"
  gitr checkout -q -b "$PROJECT_DEFAULT_BRANCH"
  printf 'readme\n' > "$REPO/README.md"
  gitr add README.md
  gitr commit -q -m "Initial commit"
  gitr push -q -u origin "$PROJECT_DEFAULT_BRANCH"
  # The remote's default branch, recorded the way a clone records it.
  gitr remote set-head origin "$PROJECT_DEFAULT_BRANCH" >/dev/null
  local row id name kind br wt
  for row in "${CASES[@]}"; do
    id="$(printf '%s' "$row" | cut -d'|' -f1)"
    name="$(printf '%s' "$row" | cut -d'|' -f2)"
    kind="$(printf '%s' "$row" | cut -d'|' -f5)"
    br="$(branch_of "$id" "$name")"; wt="$(wt_of "$id" "$name")"
    if [[ "$kind" == nowt ]]; then
      gitr branch -q "$br" "$PROJECT_DEFAULT_BRANCH"
      gitr checkout -q "$br"
      printf 'work of %s\n' "$id" > "$REPO/work-$id.txt"
      gitr add "work-$id.txt"; gitr commit -q -m "Work of $id"
      gitr checkout -q "$PROJECT_DEFAULT_BRANCH"
    else
      gitr worktree add -q -b "$br" "$wt" "$PROJECT_DEFAULT_BRANCH"
      printf 'work of %s\n' "$id" > "$wt/work-$id.txt"
      git -C "$wt" add "work-$id.txt"; git -C "$wt" commit -q -m "Work of $id"
    fi
    if [[ "$kind" != unmerged ]]; then
      gitr merge -q --no-ff -m "Merge $br" "$br" >/dev/null
    fi
    if [[ "$kind" == dirty ]]; then
      printf 'uncommitted\n' > "$wt/uncommitted.txt"
    fi
  done
  # A branch the operator made themselves: outside the Automata namespace.
  gitr worktree add -q -b "$USER_BRANCH" "$WTROOT/user-owned" "$PROJECT_DEFAULT_BRANCH"
  printf 'mine\n' > "$WTROOT/user-owned/mine.txt"
  git -C "$WTROOT/user-owned" add mine.txt
  git -C "$WTROOT/user-owned" commit -q -m "User work"
  gitr merge -q --no-ff -m "Merge $USER_BRANCH" "$USER_BRANCH" >/dev/null
}

seed_store() {
  # The create route makes drafts and no route marks one completed with a work
  # branch, so the Automata are written into the sandbox's own store before its
  # first start (the legacy prds.jsonl import path). The entry age is the
  # entry's updated_at, which is what the cleanup's retention reads.
  mkdir -p "$DATA/autonomous"
  python3 -I - "$DATA/autonomous/prds.jsonl" "$AUTOMATON_PREFIX" "$REPO" "${CASES[@]}" <<'PY'
import json, sys, datetime
out, prefix, repo, rows = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4:]
now = datetime.datetime.now(datetime.timezone.utc)
with open(out, "w") as f:
    for row in rows:
        pid, name, status, age, kind = row.split("|")
        upd = now - datetime.timedelta(hours=float(age))
        rec = {
            "id": pid, "spec": "live check sample " + name, "title": name,
            "project_dir": repo, "status": status,
            "git": {"branch": prefix + pid + "-" + name},
            "created_at": (upd - datetime.timedelta(hours=1)).isoformat(),
            "updated_at": upd.isoformat(),
        }
        f.write(json.dumps(rec) + "\n")
PY
}

write_shims() {
  # Every tmux call of the sandbox daemon goes to a socket this script owns.
  cat > "$S/bin/tmux" <<SHIM
#!/bin/sh
exec "$REAL_TMUX" -S "$TMUX_SOCK" "\$@"
SHIM
  # The worker the aider backend launches: answers --version, otherwise sleeps
  # so the session stays live until it is ended through the API.
  cat > "$S/bin/aider" <<'SHIM'
#!/bin/sh
case "$1" in --version) echo "aider live-check-stand-in"; exit 0;; esac
exec sleep 3600
SHIM
  chmod +x "$S/bin/tmux" "$S/bin/aider"
}

write_config() {
  sed \
    -e "s|data_dir: /data|data_dir: $DATA|g" \
    -e "s|default_project_dir: /data/projects|default_project_dir: $PROJ|g" \
    -e "s|port: 8080|port: $PORT|g" \
    -e "s|tls_port: 8443|tls_port: $TLS_PORT|g" \
    -e "s|sse_port: 9090|sse_port: $MCP_PORT|g" \
    -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
    -e "s|token: \"\"|token: \"$TOKEN\"|g" \
    -e "s|token: \"WEBHOOK_NOAUTH\"|token: \"\"|g" \
    -e "s|listen: \"127\.0\.0\.1:19053\".*|listen: \"127.0.0.1:$DNS_PORT\"|g" \
    -e "s|addr: 127\.0\.0\.1:19080|addr: 127.0.0.1:$HOOK_PORT|g" \
    -e "s|server_url: http://127\.0\.0\.1:19180|server_url: http://127.0.0.1:$NTFY_PORT|g" \
    "$ROOT/testdata/datawatch.yaml" > "$CFG"
  # The worktree base repository, the worktree root and the allowed project
  # root, through the normal configuration keys.
  sed -i \
    -e "s|^autonomous:\$|autonomous:\n  worktree_base_repo: $REPO\n  worktree_dir: $WTROOT|" \
    -e "s|^server:\$|server:\n  allowed_project_roots: [\"$PROJ\"]|" \
    "$CFG"
  # The stand-in worker backend.
  printf '\naider:\n  enabled: true\n  binary: %s/bin/aider\n' "$S" >> "$CFG"
}

# --- reads --------------------------------------------------------------
rest_status() { api GET /api/autonomous/cleanup; }
persisted_status() { # the status the daemon persists next to its merge record
  local f; f="$(find "$DATA" -name cleanup_status.json 2>/dev/null | head -1)"
  [[ -n "$f" ]] && cat "$f"
}
# status_json: the REST body when the route answers with the status, otherwise
# the daemon's persisted copy of the same status (src_label says which).
status_json() {
  local b; b="$(rest_status)"
  if printf '%s' "$b" | jq -e '.worktrees' >/dev/null 2>&1; then printf '%s' "$b"; return 0; fi
  persisted_status
}
src_label() { rest_status | jq -e '.worktrees' >/dev/null 2>&1 && echo "REST" || echo "the persisted status file, REST unavailable"; }
st() { status_json | jq_r "$1"; }
dlog() { cat "$LOG" "$DATA"/*.log 2>/dev/null; } # every log file the daemon writes (in foreground mode the standard logger lands in data/daemon-crash.log)
refs() { gitr for-each-ref --format='%(refname:short)' "refs/heads/" | sort | tr '\n' ' '; }
branch_exists() { gitr show-ref --verify --quiet "refs/heads/$1"; }
fmt_status() { status_json | jq -c '{worktrees:(.worktrees|{mode,removed,would_remove,skipped,failed,owner_removed,net_removed}),temp:(.temp|{mode,removed,would_remove,skipped,failed,owner_removed,net_removed}),disk_alert,disk_alert_percent}'; }
prd_detail() { api GET "/api/autonomous/prds/$1"; }
worktree_pass_ran() { [[ "$(st '.worktrees.last_run // empty')" != "" ]]; }

# Wait until the worktree pass has recorded a run NEWER than $1 (a timestamp).
wait_pass_after() { # wait_pass_after <timestamp-or-empty> <timeout>
  local after="$1" t="${2:-150}" end=$((SECONDS + ${2:-150})) cur
  while ((SECONDS < end)); do
    cur="$(st '.worktrees.last_run // empty')"
    if [[ -n "$cur" && "$cur" != "$after" ]]; then return 0; fi
    sleep 3
  done
  return 1
}

# ============================================================================
say "cleanup live check — $(now) — binary: $BINARY ($VERSION)"
say "scratch: $S   ports: http $PORT tls $TLS_PORT mcp $MCP_PORT"

LEFT_SNAP="$(mktemp /tmp/dw-cleanup-live-snap.XXXXXX)"
env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" snapshot "$LEFT_SNAP" >/dev/null 2>&1 || true

rep "# Automation cleanup — live check on a sandbox daemon"
rep ""
rep "**Date:** $(date '+%Y-%m-%d') (run started $(now), local time)"
rep "**Version under test:** ${VERSION:-unknown} (binary built from this worktree)"
rep "**Script:** \`scripts/cleanup-live-check.sh\` (run as \`env -u TMUX -u TMUX_TMPDIR bash scripts/cleanup-live-check.sh\`)"
rep "**Scope:** \`GET /api/autonomous/cleanup\`, the worktrees pass in \`report\` and then \`on\` mode, the release when an Automaton is cancelled, the daemon temp-file cleanup of worker-session scratch, the triage record and audit log entries for each removal, and the MCP, CLI and comm-channel reads, against a throwaway project repository whose default branch is \`$PROJECT_DEFAULT_BRANCH\`."

section "Setup"
build_project
write_shims
seed_store
write_config
rep ""
rep "- **Sandbox daemon only.** Started with \`td_start\` from \`scripts/test-daemon-lib.sh\` from a config generated from \`testdata/datawatch.yaml\` by the substitutions the test runner uses (data folder, ports, loopback host, test token). Added: \`autonomous.worktree_base_repo\` and \`autonomous.worktree_dir\` (the throwaway project and its worktree root), \`server.allowed_project_roots\` (one folder for the two worker sessions) and an \`aider\` backend entry that points at a stand-in worker. The production daemon, its service and its data were not touched, and nothing was ended by name."
rep "- **Ports.** HTTP $PORT, TLS $TLS_PORT, MCP $MCP_PORT, DNS $DNS_PORT, webhook $HOOK_PORT, notify $NTFY_PORT — each probed free just before use."
rep "- **tmux.** A \`tmux\` shim first on the daemon's \`PATH\` pins every tmux call to a socket this script owns (\`\$SCRATCH/tmux.sock\`). The operator's tmux server was not reachable from the daemon and no tmux command was run against it."
rep "- **Daemon temp folder.** The daemon's \`TMPDIR\` is \`\$SCRATCH/daemon-tmp\`, so its temp sweep scans only that folder."
rep "- **Everything** lives under one \`mktemp\` folder outside the worktree, removed on every exit path."
rep ""
rep "Throwaway project: a bare repository (\`remote.git\`) whose default branch is \`$PROJECT_DEFAULT_BRANCH\`, cloned to the worktree base repository; \`git remote set-head origin $PROJECT_DEFAULT_BRANCH\` records it as \`refs/remotes/origin/HEAD\`. The branch \`main\` does not exist in it."
DEFAULT_PROBE="$(gitr symbolic-ref refs/remotes/origin/HEAD 2>&1); main exists: $(gitr show-ref --verify --quiet refs/heads/main && echo yes || echo no)"
rep_block "Default branch as git records it:" "$DEFAULT_PROBE"

rep ""
rep "| Automaton id | Name | Store status | Entry age | Branch state | Expected |"
rep "|---|---|---|---|---|---|"
declare -A EXPECT=(
  [c0a10001]="removed — finished, merged, clean, old"
  [c0a10002]="removed — cancelled, merged, clean, old"
  [c0a10003]="removed — finished, merged, branch only (no work tree)"
  [c0a10004]="kept — uncommitted change in the work tree"
  [c0a10005]="kept — commit not on the default branch"
  [c0a10006]="kept — Automaton not finished (paused)"
  [c0a10007]="kept — entry younger than the retention window"
  [c0a10008]="kept (unfinished) until it is cancelled in step 5"
)
for row in "${CASES[@]}"; do
  IFS='|' read -r cid cname cstatus cage ckind <<< "$row"
  rep "| \`$cid\` | $cname | $cstatus | ${cage}h | $ckind | ${EXPECT[$cid]} |"
done
rep "| — | \`$USER_BRANCH\` (a branch the operator made) | — | — | merged, with a work tree | kept — outside the \`$AUTOMATON_PREFIX\` namespace |"

BASE_REFS="$(refs)"
BASE_WTS="$(gitr worktree list | awk '{print $1}' | sort | tr '\n' ' ')"
rep_block "Branch heads and work trees before the daemon starts:" "$(gitr for-each-ref --format='%(refname:short) %(objectname:short)' refs/heads/)
---
$(gitr worktree list)"

# --- start the sandbox daemon ---------------------------------------------
export PATH="$S/bin:$PATH" TMPDIR="$DTMP"
if ! td_start "$BINARY" "$CFG" "$LOG"; then
  verdict FAIL "the sandbox daemon started" "td_start reported a daemon that did not stay up; see the log tail: $(tail -5 "$LOG" 2>/dev/null)"
  exit 1
fi
DPID="$TD_LAST_PID"
td_set_api "$DPID" "$BASE" "$TOKEN"
if ! wait_until 90 "daemon health" curl -sk -m 3 -o /dev/null -f "$BASE/api/health"; then
  verdict FAIL "the sandbox daemon answers /api/health" "log tail: $(tail -5 "$LOG" 2>/dev/null)"
  exit 1
fi
say "  daemon pid $DPID healthy at $(now)"
rep ""
rep "Sandbox daemon pid \`$DPID\` (recorded by the lifecycle helper) was healthy at $(now)."

# ============================================================================
section "1. Report mode: would-remove is counted, nothing is removed"
R_CODE="$(api_code GET /api/autonomous/cleanup)"; R_BODY="$(rest_status)"
if [[ "$R_CODE" == 200 ]] && printf '%s' "$R_BODY" | jq -e '.worktrees and .temp' >/dev/null 2>&1; then
  verdict PASS "\`GET /api/autonomous/cleanup\` answers 200 with both passes" "$(printf '%s' "$R_BODY" | jq -c '{worktrees:.worktrees.mode, temp:.temp.mode}')"
else
  verdict FAIL "\`GET /api/autonomous/cleanup\` answers 200 with both passes" "HTTP $R_CODE, body: \`$(printf '%s' "$R_BODY" | head -c 200)\`. The checks below read the daemon's persisted copy of the same status instead (the file the daemon writes beside its merge record), so the passes themselves can still be judged."
fi
if [[ "$R_CODE" != 200 ]]; then
  rep_block "Static diagnosis of the unavailable route (what the server can call on the object it holds):" "$(grep -n 'func (s \*Server) handleCleanupStatus' -A22 "$ROOT/internal/server/cleanup.go" | grep -n 'cleanupStatusReader\|unavailable' )
--- the server holds *autonomous.API (SetAutonomousAPI); its pass-through methods for the merge reads and for the cleanup read:
$(grep -n 'func (a \*API) \(MergeStatus\|MergeSafeToDelete\|CleanupStatus\)' "$ROOT/internal/autonomous/api.go" || true)
--- the method the route needs:
$(grep -n 'func (m \*Manager) CleanupStatus' "$ROOT/internal/autonomous/cleanup_status.go")"
fi
if ! wait_until 150 "first worktree pass" worktree_pass_ran; then
  verdict FAIL "the worktrees pass ran after the daemon started" "no last_run within 150 s; status ($(src_label)): $(status_json)"
else
  S1="$(status_json)"
  rep_block "Cleanup status (read from: $(src_label)), mode left at its default:" "$(printf '%s' "$S1" | jq .)"
  M="$(printf '%s' "$S1" | jq_r '.worktrees.mode')"
  W="$(printf '%s' "$S1" | jq_r '.worktrees.would_remove')"
  R="$(printf '%s' "$S1" | jq_r '.worktrees.removed')"
  SK="$(printf '%s' "$S1" | jq_r '.worktrees.skipped')"
  [[ "$M" == report ]] && verdict PASS "the unset mode runs as report" "mode=\`$M\`" || verdict FAIL "the unset mode runs as report" "mode=\`$M\`"
  [[ "$W" == 3 && "$R" == 0 ]] \
    && verdict PASS "the three removable Automaton branches are counted as would-remove and none is removed" "would_remove=$W removed=$R skipped=$SK" \
    || verdict FAIL "the three removable Automaton branches are counted as would-remove and none is removed" "would_remove=$W removed=$R skipped=$SK"
  [[ "$SK" == 5 ]] \
    && verdict PASS "the five held entries (dirty, unmerged, two unfinished, young) are counted as skipped" "skipped=$SK" \
    || verdict FAIL "the five held entries (dirty, unmerged, two unfinished, young) are counted as skipped" "skipped=$SK"
  NOW_REFS="$(refs)"; NOW_WTS="$(gitr worktree list | awk '{print $1}' | sort | tr '\n' ' ')"
  if [[ "$NOW_REFS" == "$BASE_REFS" && "$NOW_WTS" == "$BASE_WTS" ]]; then
    verdict PASS "report mode removed no branch and no work tree" "branches: $NOW_REFS"
  else
    verdict FAIL "report mode removed no branch and no work tree" "before: $BASE_REFS / $BASE_WTS — after: $NOW_REFS / $NOW_WTS"
  fi
  LOGLINE="$(dlog | grep -E 'worktree-cleanup|temp-sweep' | head -4)"
  rep_block "Daemon log, cleanup summary lines:" "$LOGLINE"
fi
SAFE_REST="$(api GET /api/autonomous/safe-to-delete)"
rep_block "\`GET /api/autonomous/safe-to-delete\` on the same project (for comparison with the cleanup's own count):" "$(printf '%s' "$SAFE_REST" | jq -c . 2>/dev/null || printf '%s' "$SAFE_REST")"

# ============================================================================
section "2. The four reads agree (report mode)"
SURF_PY="$S/surfaces.py"
cat > "$SURF_PY" <<'PY'
import json, sys
rest, cli_json, mcp_text, cli_text, comm_text = [open(p).read() for p in sys.argv[1:6]]
problems = []
try:
    r = json.loads(rest)
    c = json.loads(cli_json)
    m = json.loads(mcp_text)
except Exception as e:
    print("a read did not return the status: REST said %r, MCP said %r" % (rest[:80].strip(), mcp_text[:80].strip())); sys.exit(1)
canon = lambda d: json.dumps(d, sort_keys=True)
if canon(r) != canon(c): problems.append("CLI --json differs from REST")
if canon(r) != canon(m): problems.append("MCP result differs from REST")
for label, text in (("CLI text", cli_text), ("comm reply", comm_text)):
    for name, key in (("worktrees", "worktrees"), ("temp", "temp")):
        p = r[key]
        if p.get("last_run") is None:
            want = "%s pass: not run yet" % name
        else:
            want = "%s pass (mode %s): last run %s, removed %d, would-remove %d, skipped %d, failed %d, freed %d bytes" % (
                name, p["mode"] or "report", p["last_run"], p["removed"], p["would_remove"], p["skipped"], p["failed"], p["bytes_freed"])
        # the rendered time is RFC 3339 in UTC or local form; compare the counts part only
        counts = want.split("last run")[0] if "last run" in want else want
        tail = want.split(", removed", 1)[1] if ", removed" in want else ""
        if counts not in text or (tail and (", removed" + tail) not in text):
            problems.append("%s misses the %s line (%s)" % (label, name, want))
print("OK" if not problems else "; ".join(problems))
sys.exit(0 if not problems else 1)
PY

mcp_read() { # mcp_read -> prints the autonomous_cleanup_status result text
  python3 -I - "$BINARY" "$CFG" <<'PY'
import json, subprocess, sys, os, select, time
bin_, cfg = sys.argv[1], sys.argv[2]
env = {k: v for k, v in os.environ.items() if k not in ("TMUX", "TMUX_TMPDIR")}
p = subprocess.Popen([bin_, "--config", cfg, "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                     stderr=subprocess.DEVNULL, env=env, text=True)
def send(o): p.stdin.write(json.dumps(o) + "\n"); p.stdin.flush()
send({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2024-11-05", "capabilities": {}, "clientInfo": {"name": "cleanup-live-check", "version": "0"}}})
send({"jsonrpc": "2.0", "method": "notifications/initialized"})
send({"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": "autonomous_cleanup_status", "arguments": {}}})
end = time.time() + 30
out = ""
while time.time() < end:
    r, _, _ = select.select([p.stdout], [], [], 1)
    if not r: continue
    line = p.stdout.readline()
    if not line: break
    try: msg = json.loads(line)
    except Exception: continue
    if msg.get("id") == 2:
        res = msg.get("result", {})
        out = "".join(c.get("text", "") for c in res.get("content", []))
        break
try: p.stdin.close()
except Exception: pass
try: p.wait(timeout=5)
except Exception: p.kill()
print(out)
PY
}

compare_surfaces() { # compare_surfaces <label>
  local label="$1" try v ev
  for try in 1 2 3; do
    api GET /api/autonomous/cleanup > "$S/s-rest.json"
    dw autonomous cleanup --json > "$S/s-cli.json" 2> "$S/s-cli.err"
    mcp_read > "$S/s-mcp.json"
    dw autonomous cleanup > "$S/s-cli.txt" 2>> "$S/s-cli.err"
    api POST /api/test/message '{"text":"autonomous cleanup"}' | jq -r '.responses | join("\n")' > "$S/s-comm.txt" 2>/dev/null
    v="$(python3 -I "$SURF_PY" "$S/s-rest.json" "$S/s-cli.json" "$S/s-mcp.json" "$S/s-cli.txt" "$S/s-comm.txt" 2>&1)"
    [[ "$v" == OK ]] && break
    sleep 2   # a poll may have landed between two of the four reads
  done
  ev="REST: \`GET /api/autonomous/cleanup\`; CLI: \`datawatch autonomous cleanup --json\` and \`datawatch autonomous cleanup\`; MCP: \`autonomous_cleanup_status\` over \`datawatch mcp\` (stdio); comm: \`POST /api/test/message {\"text\":\"autonomous cleanup\"}\`. Comparison result: \`$v\`"
  rep_block "CLI text:" "$(cat "$S/s-cli.txt")"
  rep_block "Comm-channel reply:" "$(cat "$S/s-comm.txt")"
  rep_block "MCP result (first 600 bytes):" "$(head -c 600 "$S/s-mcp.json")"
  if [[ "$v" == OK ]]; then verdict PASS "REST, MCP, CLI and comm reads agree ($label)" "$ev"
  else verdict FAIL "REST, MCP, CLI and comm reads agree ($label)" "$ev; CLI stderr: $(head -c 300 "$S/s-cli.err")"; fi
}
compare_surfaces "report mode"

# ============================================================================
section "3. A setting that is not allowed is refused and saves nothing"
BEFORE_CFG="$(api GET /api/autonomous/config | jq -c '.automation // {}')"
C0="$(api_code PUT /api/autonomous/config '{"automation":{"cleanup_retention_hours":0}}')"
C1="$(api_code PUT /api/autonomous/config '{"automation":{"cleanup_mode":"sometimes"}}')"
C2="$(api_code PUT /api/autonomous/config '{"automation":{"disk_alert_percent":101}}')"
AFTER_CFG="$(api GET /api/autonomous/config | jq -c '.automation // {}')"
if [[ "$C0" == 400 && "$C1" == 400 && "$C2" == 400 && "$BEFORE_CFG" == "$AFTER_CFG" ]]; then
  verdict PASS "retention 0, mode \`sometimes\` and disk percent 101 are refused with 400 and nothing is saved" "HTTP $C0 / $C1 / $C2; automation settings identical before and after"
else
  verdict FAIL "retention 0, mode \`sometimes\` and disk percent 101 are refused with 400 and nothing is saved" "HTTP $C0 / $C1 / $C2; before: $BEFORE_CFG after: $AFTER_CFG"
fi
rep ""
rep "Note: the brief asked for \"retention 0\". The smallest retention the cleanup accepts is 1 hour (0 is refused above, and an unset or hand-edited 0 reads as the 24-hour default), so the on-mode step below uses 1 hour and the sample Automata are 72 hours old."

# ============================================================================
section "4. On mode: only the safe entries are removed"
LAST_BEFORE="$(st '.worktrees.last_run // empty')"
PUTC="$(api PUT /api/config '{"autonomous.automation.cleanup_retention_hours":1,"autonomous.automation.cleanup_mode":"on"}')"
rep_block "\`PUT /api/config\` (retention 1 hour, mode on):" "$PUTC"
PUT_TS="$(date +%s)"
FIRST_BR="$(branch_of c0a10001 release-completed)"
gone_first() { ! branch_exists "$FIRST_BR"; }
wait_until 150 "first removal" gone_first
GONE_AT="$(date +%s)"
# Read the triage records straight away: the coordinator clears a stored record
# on the poll after the removal when the merge-pending mode is off.
TR_OK=1; TR_TXT=""
for id in c0a10001 c0a10002 c0a10003; do
  note=""
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    d="$(prd_detail "$id")"
    note="$(printf '%s' "$d" | jq_r '[.triage.notes[]? | select(.source=="worktree-cleanup") | .text] | join(" | ")')"
    [[ -n "$note" ]] && break
    sleep 1
  done
  if [[ -z "$note" ]]; then TR_OK=0; TR_TXT+="$id: no worktree-cleanup note; "; else TR_TXT+="$id: $note; "; fi
done
TR_AGE=$(( $(date +%s) - GONE_AT ))
if wait_pass_after "$LAST_BEFORE" 150; then
  S2="$(status_json)"
  rep_block "Cleanup status after the pass (read from: $(src_label)):" "$(printf '%s' "$S2" | jq .)"
  [[ "$(printf '%s' "$S2" | jq_r '.worktrees.mode')" == on ]] \
    && verdict PASS "the pass picked up mode on without a restart" "mode=on" \
    || verdict FAIL "the pass picked up mode on without a restart" "mode=$(printf '%s' "$S2" | jq_r '.worktrees.mode')"
else
  S2="$(status_json)"
  verdict FAIL "the pass picked up mode on without a restart" "no new pass recorded within 150 s; status ($(src_label)): $S2"
fi
GONE_OK=1; GONE_TXT=""
for id in c0a10001 c0a10002 c0a10003; do
  n="$(case_field "$id" 2)"; br="$(branch_of "$id" "$n")"
  if branch_exists "$br"; then GONE_OK=0; GONE_TXT+="$br still exists; "; fi
  [[ -d "$(wt_of "$id" "$n")" ]] && { GONE_OK=0; GONE_TXT+="work tree of $id still exists; "; }
done
[[ $GONE_OK == 1 ]] \
  && verdict PASS "the three safe entries are gone (two with their work trees, one branch only)" "removed=$(st '.worktrees.removed') would_remove=$(st '.worktrees.would_remove') skipped=$(st '.worktrees.skipped') failed=$(st '.worktrees.failed')" \
  || verdict FAIL "the three safe entries are gone" "$GONE_TXT removed=$(st '.worktrees.removed') failed=$(st '.worktrees.failed') last_error=$(st '.worktrees.last_error')"

KEEP_OK=1; KEEP_TXT=""
for id in c0a10004 c0a10005 c0a10006 c0a10007 c0a10008; do
  n="$(case_field "$id" 2)"; br="$(branch_of "$id" "$n")"
  branch_exists "$br" || { KEEP_OK=0; KEEP_TXT+="$br was removed; "; }
  [[ -d "$(wt_of "$id" "$n")" ]] || { KEEP_OK=0; KEEP_TXT+="work tree of $id was removed; "; }
done
branch_exists "$USER_BRANCH" && [[ -d "$WTROOT/user-owned" ]] || { KEEP_OK=0; KEEP_TXT+="the user branch or its work tree was removed; "; }
DIRTY_WT="$(wt_of c0a10004 held-dirty)"
[[ -f "$DIRTY_WT/uncommitted.txt" ]] || { KEEP_OK=0; KEEP_TXT+="the uncommitted file was lost; "; }
[[ $KEEP_OK == 1 ]] \
  && verdict PASS "the dirty, unmerged, unfinished and young entries and the operator's own branch were all kept" "remaining branches: $(refs)" \
  || verdict FAIL "the dirty, unmerged, unfinished and young entries and the operator's own branch were all kept" "$KEEP_TXT remaining: $(refs)"

# The default branch is the project's own, and no branch of any other name was assumed.
[[ "$(gitr rev-parse --abbrev-ref HEAD)" == "$PROJECT_DEFAULT_BRANCH" ]] && ! branch_exists main \
  && verdict PASS "the cleanup judged merged work against \`$PROJECT_DEFAULT_BRANCH\` (the repository has no \`main\`)" "HEAD of the base repository: $(gitr rev-parse --abbrev-ref HEAD)" \
  || verdict FAIL "the cleanup judged merged work against \`$PROJECT_DEFAULT_BRANCH\`" "HEAD: $(gitr rev-parse --abbrev-ref HEAD)"

# Audit log (the triage notes were read right after the removal, above).
AUD="$(api GET '/api/audit?action=autonomous_worktree_remove&limit=50')"
AUD_N="$(printf '%s' "$AUD" | jq_r '.count')"
AUD_BR="$(printf '%s' "$AUD" | jq_r '[.entries[].details.branch] | sort | join(" ")')"
EXP_BR="$(for id in c0a10001 c0a10002 c0a10003; do branch_of "$id" "$(case_field "$id" 2)"; echo; done | sort | tr '\n' ' ' | sed 's/ *$//')"
rep_block "Audit log, \`GET /api/audit?action=autonomous_worktree_remove\`:" "$(printf '%s' "$AUD" | jq -c '.entries[] | {at:.ts, actor, action, branch:.details.branch, bytes_freed:.details.bytes_freed, base_branch:.details.base_branch, age_seconds:.details.age_seconds}')"
[[ "$AUD_N" == 3 && "$AUD_BR" == "$(printf '%s' "$EXP_BR" | tr ' ' '\n' | sort | tr '\n' ' ' | sed 's/ *$//')" ]] \
  && verdict PASS "each of the three removals is in the audit log (action \`autonomous_worktree_remove\`, with branch, base branch, age and bytes freed)" "$AUD_BR" \
  || verdict FAIL "each of the three removals is in the audit log" "count=$AUD_N branches=$AUD_BR expected=$EXP_BR"
TRIAGE_DEFER=""
if [[ $TR_OK == 1 ]]; then
  verdict PASS "each removal is in the Automaton's triage record" "$TR_TXT (read $TR_AGE s after the branch was seen gone)"
else
  verdict FAIL "each removal is in the Automaton's triage record" "$TR_TXT (read $TR_AGE s after the branch was seen gone; a stored record may also be cleared by the next poll when the merge-pending mode is off)"
fi
rep_block "Daemon log, cleanup lines so far (the log holds $(dlog | wc -l) lines, $(dlog | grep -c '\[autonomous\]') of them from the Automata engine):" "$(dlog | grep -E 'worktree-cleanup|temp-sweep|cleanup status' | tail -8)"
rep_block "Daemon log, every line from the Automata engine ([autonomous]):" "$(dlog | grep '\[autonomous\]' | cut -c1-220 | tail -14)"
LOGFILES="$(for f in "$LOG" "$DATA"/*.log "$DATA"/**/*.log; do [[ -f "$f" ]] && printf '%s: %s bytes, %s lines, %s cleanup summary lines\n' "${f#"$S"/}" "$(stat -c %s "$f")" "$(wc -l < "$f")" "$(grep -c -E 'worktree-cleanup (pass|release)|temp-sweep:' "$f")"; done)"
rep_block "Log files the sandbox daemon wrote:" "$LOGFILES"
SUMLINES="$(dlog | grep -c -E 'worktree-cleanup pass: mode="on"')"
[[ "$SUMLINES" -ge 1 ]] \
  && verdict PASS "the daemon log carries a summary line for the worktrees pass" "$(dlog | grep -E 'worktree-cleanup pass: mode="on"' | tail -1) — found in: $(grep -l -E 'worktree-cleanup pass: mode="on"' "$LOG" "$DATA"/*.log 2>/dev/null | sed "s|^$S/||" | tr '\n' ' ')" \
  || verdict FAIL "the daemon log carries a summary line for the worktrees pass (the how-to says each pass writes one)" "no \`worktree-cleanup pass\` line in any log file the daemon wrote, although the audit stream holds the same aggregate for each pass"

# ============================================================================
section "5. Release at end: cancelling an Automaton calls the same release"
REL_BR="$(branch_of c0a10008 end-release)"
REL_BEFORE="$(status_json | jq -c '.worktrees | {last_run, skipped, owner_removed, net_removed}')"
C_CODE="$(api_code POST /api/autonomous/prds/c0a10008/cancel)"
sleep 3
CANCELLED="$(prd_detail c0a10008 | jq_r '.status')"
REL_LINE="$(dlog | grep -E 'worktree-cleanup (release|release_skipped)' | tail -2)"
rep_block "\`POST /api/autonomous/prds/c0a10008/cancel\` → HTTP $C_CODE, Automaton status \`$CANCELLED\`. Daemon log (release lines):" "$REL_LINE"
rep_block "Cancelled entry, \`updated_at\`, branch state and the worktrees counters before and after (read from: $(src_label)):" "updated_at=$(prd_detail c0a10008 | jq_r '.updated_at'); entry age at cancel: under 1 minute; branch exists: $(branch_exists "$REL_BR" && echo yes || echo no)
before: $REL_BEFORE
after:  $(status_json | jq -c '.worktrees | {last_run, skipped, owner_removed, net_removed}')"
REL_AUD="$(api GET '/api/audit?action=autonomous_worktree_cleanup_release*&limit=5' | jq -c '.entries[]? | {at:.ts, action, details}')"
rep_block "Audit log, the release outcome the cancel wrote (\`autonomous_worktree_cleanup_release*\`):" "${REL_AUD:-none}"
if [[ "$C_CODE" == 200 && "$CANCELLED" == cancelled ]] && ! branch_exists "$REL_BR" && [[ ! -d "$(wt_of c0a10008 end-release)" ]]; then
  verdict PASS "cancelling an Automaton whose work is already on the default branch released its branch and work tree at once" "owner_removed=$(st '.worktrees.owner_removed')"
else
  verdict FAIL "cancelling an Automaton whose work is already on the default branch released its branch and work tree at once" "cancel HTTP $C_CODE, status $CANCELLED, branch still exists: $(branch_exists "$REL_BR" && echo yes || echo no); release log: $(printf '%s' "$REL_LINE" | tail -1)"
fi

# ============================================================================
section "6. Daemon temp files: kept while the session is live, gone when it ends"
start_session() { # start_session <name> -> prints the full id
  local pd="$PROJ/$1"; mkdir -p "$pd"
  api POST /api/sessions/start "$(python3 -I -c 'import json,sys;print(json.dumps({"task":"cleanup live check "+sys.argv[2],"project_dir":sys.argv[1],"backend":"aider","name":sys.argv[2],"one_shot":True}))' "$pd" "$1")" | jq_r '.full_id // empty'
}
SID_LIVE="$(start_session live-session)"
SID_END="$(start_session ending-session)"
rep "- Two worker sessions on the \`aider\` backend (a stand-in that sleeps): live id \`${SID_LIVE:-none}\`, to-be-ended id \`${SID_END:-none}\`."
task_file_for() { # task_file_for <session full id> -> the scratch file whose name carries that session id
  local f
  for f in "$DTMP"/datawatch-aider-task-*-"$1"-*.txt; do [[ -e "$f" ]] && { printf '%s' "$f"; return 0; }; done
  return 1
}
FOREIGN="$DTMP/datawatch-foreign-check"
mkdir -p "$FOREIGN" && printf 'not made by the daemon\n' > "$FOREIGN/note.txt"
if [[ -n "$SID_LIVE" && -n "$SID_END" ]]; then
  F_LIVE=""; F_END=""
  for _ in $(seq 1 30); do
    F_LIVE="$(task_file_for "$SID_LIVE" || true)"; F_END="$(task_file_for "$SID_END" || true)"
    [[ -n "$F_LIVE" && -n "$F_END" ]] && break
    sleep 2
  done
  rep_block "Scratch files in the daemon's temp folder after both sessions started:" "$(ls -1 "$DTMP")"
  if [[ -n "$F_LIVE" && -n "$F_END" ]]; then
    verdict PASS "each worker session left its own prefixed scratch file in the daemon's temp folder" "live: \`$(basename "$F_LIVE")\`; to-be-ended: \`$(basename "$F_END")\`"
    KILL_CODE="$(api_code POST /api/sessions/kill "{\"id\":\"$SID_END\"}")"
    for _ in $(seq 1 20); do [[ -e "$F_END" ]] || break; sleep 1; done
    [[ ! -e "$F_END" ]] \
      && verdict PASS "the scratch file of the ended session is gone (session end released it)" "kill HTTP $KILL_CODE; owner_removed on the temp pass: $(st '.temp.owner_removed')" \
      || verdict FAIL "the scratch file of the ended session is gone" "kill HTTP $KILL_CODE; still present: $F_END"
    [[ -e "$F_LIVE" ]] \
      && verdict PASS "the scratch file of the live session is still there" "$(basename "$F_LIVE")" \
      || verdict FAIL "the scratch file of the live session is still there" "missing: $F_LIVE"
    # Let at least one more pass run, with mode on, and check again.
    T_BEFORE="$(st '.temp.last_run // empty')"
    temp_pass_advanced() { [[ "$(st '.temp.last_run // empty')" != "$T_BEFORE" ]]; }
    wait_until 150 "temp pass" temp_pass_advanced
    rep_block "Status after another pass with a live session and a foreign name present:" "$(fmt_status)"
    [[ -e "$F_LIVE" && -d "$FOREIGN" && -f "$FOREIGN/note.txt" && "$(st '.temp.mode')" == on ]] \
      && verdict PASS "a pass in mode on kept the live session's file and a prefixed name the daemon never created" "temp: removed=$(st '.temp.removed') skipped=$(st '.temp.skipped') failed=$(st '.temp.failed')" \
      || verdict FAIL "a pass in mode on kept the live session's file and a prefixed name the daemon never created" "live file: $([[ -e "$F_LIVE" ]] && echo present || echo gone); foreign: $([[ -d "$FOREIGN" ]] && echo present || echo gone); temp mode: $(st '.temp.mode')"
    [[ "$(st '.temp.skipped')" -ge 2 ]] \
      && verdict PASS "the pass counted the two names it kept (the live session's file and the foreign name) as skipped" "temp skipped=$(st '.temp.skipped')" \
      || verdict FAIL "the pass counted the two names it kept (the live session's file and the foreign name) as skipped" "temp skipped=$(st '.temp.skipped'); last_run=$(st '.temp.last_run')"
    KILL2="$(api_code POST /api/sessions/kill "{\"id\":\"$SID_LIVE\"}")"
    for _ in $(seq 1 20); do [[ -e "$F_LIVE" ]] || break; sleep 1; done
    [[ ! -e "$F_LIVE" ]] \
      && verdict PASS "once the live session ended, its file went too" "kill HTTP $KILL2" \
      || verdict FAIL "once the live session ended, its file went too" "kill HTTP $KILL2; present: $F_LIVE"
  else
    verdict FAIL "each worker session left its own prefixed scratch file in the daemon's temp folder" "live=${F_LIVE:-none} ended=${F_END:-none}; folder: $(ls -1 "$DTMP" | tr '\n' ' '); daemon log tail: $(dlog | tail -3)"
  fi
else
  verdict FAIL "two worker sessions started on the stand-in backend" "live=${SID_LIVE:-none} ending=${SID_END:-none}; daemon log tail: $(dlog | tail -3)"
fi
rep ""
rep "Not shown live: removal by the periodic temp sweep itself. It only removes scratch of a session that ended at least the retention window ago (minimum 1 hour), so a sweep removal cannot happen inside a short check. The sweep's kept cases (live session, foreign name) are shown above; its removal rule is covered by the unit tests in \`internal/autonomous/temp_sweep_test.go\`."

# ============================================================================
section "7. The four reads agree (on mode, after the removals)"
compare_surfaces "on mode"

# ============================================================================
section "Cleanup"
# End the daemon (and its sessions) by the recorded PID only.
td_stop_all
sleep 1
D_GONE="no"; td_alive "$DPID" || D_GONE="yes"
PORT_CLOSED="no"; td_wait_port_closed "$TLS_PORT" 20 && PORT_CLOSED="yes"
rep ""
rep "- **Sandbox daemon ended** by \`td_stop_all\` (recorded pid \`$DPID\`, marker = its config path). Process gone: $D_GONE. TLS port $TLS_PORT closed: $PORT_CLOSED."
if [[ -S "$TMUX_SOCK" ]]; then
  "$REAL_TMUX" -S "$TMUX_SOCK" kill-server >/dev/null 2>&1 || true
fi
TMUX_LEFT="no"; [[ -S "$TMUX_SOCK" ]] && "$REAL_TMUX" -S "$TMUX_SOCK" ls >/dev/null 2>&1 && TMUX_LEFT="yes"
rep "- **Private tmux server** (explicit throwaway socket) ended; a session left on it: $TMUX_LEFT."
LEFT_OUT="$(env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" check "$LEFT_SNAP" 2>&1)"; LEFT_RC=$?
rep "- **Leftover check** against the snapshot taken before this run: \`scripts/check-no-leftover-test-daemons.sh check\` exit $LEFT_RC — $(printf '%s' "$LEFT_OUT" | tail -1)"
rm -f "$LEFT_SNAP"
if [[ "$D_GONE" == yes && "$PORT_CLOSED" == yes && "$TMUX_LEFT" == no && "$LEFT_RC" == 0 ]]; then
  verdict PASS "the sandbox daemon, its sessions and its tmux server are gone and no test daemon was left behind" "daemon gone: $D_GONE; port closed: $PORT_CLOSED; private tmux session left: $TMUX_LEFT; leftover check exit $LEFT_RC"
else
  verdict FAIL "the sandbox daemon, its sessions and its tmux server are gone and no test daemon was left behind" "daemon gone: $D_GONE; port closed: $PORT_CLOSED; private tmux session left: $TMUX_LEFT; leftover check exit $LEFT_RC: $LEFT_OUT"
fi
rep "- **Scratch area** (project, remote, work trees, data, config, log, daemon records, shims) is removed by the exit handler right after this report is written."

# --- summary and report ------------------------------------------------------
{
  printf '# Automation cleanup — live check on a sandbox daemon\n\n'
} > /dev/null
SUMMARY="## Summary

| # | Result | Check |
|---|---|---|"
for r in "${RESULTS[@]}"; do
  IFS='|' read -r n v d <<< "$r"
  SUMMARY+=$'\n'"| $n | $v | $d |"
done
SUMMARY+=$'\n\n'"**Checks:** $PASS passed, $FAIL failed."
OVERALL="PASS"; [[ $FAIL -gt 0 ]] && OVERALL="FAIL"

# Place the summary right after the title block (first 6 lines of the buffer).
mkdir -p "$(dirname "$REPORT")"
{
  head -n 6 "$REPORT_BUF"
  printf '\n%s\n' "$SUMMARY"
  tail -n +7 "$REPORT_BUF"
  printf '\n\nResult: %s\n' "$OVERALL"
} > "$REPORT"

say ""
say "Summary: $PASS passed, $FAIL failed — report: $REPORT"
[[ $FAIL -eq 0 ]]
