#!/usr/bin/env bash
# supervisor-loop-archive-live-check.sh — live check of the supervisor loop's
# grace period for archiving cancelled Automata, on a SANDBOX daemon, against
# a throwaway project repository.
#
# What it proves (each result is a numbered check in the report):
#   1. Config: `autonomous.supervisor_loop.archive_cancelled_after_minutes`
#      round-trips through the config API — unset reads as the default 60,
#      PUT 60 reads back 60, and a write below zero is refused with a 400
#      that saves nothing.
#   2. Grace holds: an Automaton whose branch still carries a commit that is
#      not on the default branch is cancelled (actor "operator") and survives
#      a supervisor-loop `close_finished_records` pass with the grace at 60
#      — the pass reports it as held, and the branch work is untouched.
#   3. Immediate archive: with the grace at 0, a cancelled Automaton whose
#      branch carries no work is archived by that same pass with the "archive"
#      decision recorded under the actor "supervisor-loop" — while the branch-
#      carrying Automaton from check 2 is still kept, because unmerged branch
#      work outweighs any grace value.
#   4. Cleanup: the sandbox daemon and its sessions are gone, no test daemon
#      was left behind, and the scratch area is removed on every exit path.
#
# How the states are made. The worker is the daemon's own shell backend
# pointed at a small generated script (shell_backend.script_path), so a worker
# runs headlessly and deterministically. The two Automata are written into
# the sandbox's own store before its first start: the create route only makes
# drafts and a plan needs a planner model, which a sandbox does not have.
# From the moment the daemon is up everything goes through its REST API
# (approve, run, cancel, settings, the manual job run, the run records).
# The worktrees and branches are made by the daemon itself when a run starts
# (autonomous.worktree_base_repo); the session's own commit is what puts the
# one work commit on the branch — a session with nothing to stage never
# commits, so the no-work Automaton's branch stays exactly where the default
# branch is.
#
# Generality: nothing here names this repository. The project is a throwaway
# repository created inside the project's .datawatch folder whose default
# branch is NOT "main" (the supervisor loop resolves the base branch from
# refs/remotes/origin/HEAD).
#
# Safety: SANDBOX only. The daemon runs from a generated config under a
# throwaway directory inside the project's .datawatch folder, on loopback
# ports, with its own data directory, its own TMPDIR and its own multiplexer
# socket directory (the script itself also strips its multiplexer variables,
# so no session of the operator is reachable). Production port 8443 and the
# production daemon, service and data are never touched. The daemon is ended
# only by the PID that scripts/test-daemon-lib.sh recorded, with the config
# path as marker, on every exit path.
#
# Usage (always from a shell with the operator's multiplexer variables removed):
#   env -u TMUX -u TMUX_TMPDIR bash scripts/supervisor-loop-archive-live-check.sh
#
# Env:
#   ARCHIVE_LIVE_CHECK_REPORT  where the tracked report is written
#                             (default: docs/plans/2026-11-06-supervisor-loop-archive-grace-live-check-report.md)
#   TEST_BINARY                daemon binary (default: bin/datawatch, built from
#                              this worktree when missing, then PATH)
#
# Exit 0 only when every check passed.
#
# The report is regenerated on every run, so nothing in it depends on a hand
# edit.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/scripts/test-daemon-lib.sh"
REPORT="${ARCHIVE_LIVE_CHECK_REPORT:-$ROOT/docs/plans/2026-11-06-supervisor-loop-archive-grace-live-check-report.md}"

GRACE_KEY="autonomous.supervisor_loop.archive_cancelled_after_minutes"
# The throwaway project's default branch: deliberately not "main".
PROJECT_DEFAULT_BRANCH="trunk"
# The directory both Automata declare as their write scope.
WRITE_SUBDIR="src"
# The deterministic verification script every task carries (tracked in the project).
VERIFY_SCRIPT=".checks/ok.sh"
# The two seeded Automata.
A_WORK="e2a10001"    # the worker commits a file: the branch carries unmerged work
A_NOWORK="e2a10002"  # the worker writes nothing: the branch carries no work

unset TMUX TMUX_TMPDIR

# --- scratch area and lifecycle ----------------------------------------------
# The throwaway project lives inside the project's own .datawatch folder.
S="$ROOT/.datawatch/throwaway-archive-live-check"
rm -rf "$S"
export TD_STATE_DIR="$S/tdstate"
# shellcheck source=/dev/null
source "$LIB"

check_cleanup_extra() {
  # Runs AFTER td_stop_all (the daemon is gone): remove the scratch area.
  rm -rf "$S" 2>/dev/null || true
}
TD_EXTRA_CLEANUP=check_cleanup_extra
td_install_traps "$@"

# --- result bookkeeping --------------------------------------------------------
CUR=0                       # check number the report text goes under (0 = setup)
declare -A CK_TOTAL=() CK_FAIL=() CK_TITLE=()

say() { printf '%s\n' "$*"; }
rep() { printf '%s\n' "$*" >> "$S/rep-$CUR.md"; }
rep_block() { # rep_block <label> <text>: fenced observed output, bounded
  rep ""
  rep "$1"
  rep '```'
  printf '%s\n' "$2" | head -c 3000 >> "$S/rep-$CUR.md"
  printf '\n' >> "$S/rep-$CUR.md"
  rep '```'
}
begin_check() { # begin_check <n> <title>
  CUR="$1"; CK_TITLE[$1]="$2"; CK_TOTAL[$1]=0; CK_FAIL[$1]=0
  say ""; say "== $1. $2 =="
  rep ""; rep "## $1. $2"
}
ck() { # ck <n> <0 for pass> <what> [evidence]
  local n="$1" ok="$2" what="$3" ev="${4:-}" v=PASS
  CK_TOTAL[$n]=$(( ${CK_TOTAL[$n]:-0} + 1 ))
  if [[ "$ok" != 0 ]]; then v=FAIL; CK_FAIL[$n]=$(( ${CK_FAIL[$n]:-0} + 1 )); fi
  say "  $v  [$n] $what"
  rep ""
  rep "- **$v** — $what"
  if [[ -n "$ev" ]]; then rep "  - $ev"; fi
}
now() { date '+%H:%M:%S'; }

# --- prerequisites ---------------------------------------------------------
for t in git curl python3 jq; do
  command -v "$t" >/dev/null 2>&1 || { say "SKIP: $t is not on PATH"; exit 0; }
done

BINARY=""
if [[ -n "${TEST_BINARY:-}" && -x "${TEST_BINARY:-}" ]]; then
  BINARY="$TEST_BINARY"
elif [[ -x "$ROOT/bin/datawatch" ]]; then
  BINARY="$ROOT/bin/datawatch"
elif command -v go >/dev/null 2>&1 && (cd "$ROOT" && env -u TMUX -u TMUX_TMPDIR go build -o bin/datawatch ./cmd/datawatch/) >/dev/null 2>&1; then
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
WTROOT="$S/worktrees"       # the worktree root (autonomous.worktree_dir); one folder per Automaton
DATA="$S/data"
PROJ="$S/projects"          # allowed project root
DTMP="$S/daemon-tmp"        # the daemon's own TMPDIR
SOCKDIR="$S/run"            # the daemon's multiplexer socket directory
STATE="$S/worker-state"     # what the stand-in worker remembers between runs
CFG="$S/config.yaml"
LOG="$S/daemon.log"
mkdir -p "$WTROOT" "$DATA" "$PROJ" "$DTMP" "$SOCKDIR" "$STATE" "$S/bin"

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
api_both() { # api_both <METHOD> <path> <json body> — "<code> <body>"
  local out; out="$(curl -sk -m 30 -w '\n%{http_code}' -X "$1" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$3" "$BASE$2")"
  printf '%s %s' "${out##*$'\n'}" "${out#*$'\n'}"
}
jq_r() { jq -r "$@" 2>/dev/null; }

wait_until() { # wait_until <timeout-seconds> <command...>
  local t="$1"; shift
  local end=$((SECONDS + t))
  while ((SECONDS < end)); do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 2
  done
  return 1
}

gitr() { git -C "$REPO" "$@"; }
wt_of() { printf '%s/%s' "$WTROOT" "$1"; }
wgit() { local id="$1"; shift; git -C "$(wt_of "$id")" "$@"; }

# --- the sandbox pieces -------------------------------------------------------
build_project() {
  git init -q --bare -b "$PROJECT_DEFAULT_BRANCH" "$REMOTE"
  git clone -q "$REMOTE" "$REPO" 2>/dev/null
  gitr config user.email "archive-live-check@example.invalid"
  gitr config user.name "supervisor loop archive live check"
  gitr config commit.gpgsign false
  gitr checkout -q -b "$PROJECT_DEFAULT_BRANCH"
  mkdir -p "$REPO/$WRITE_SUBDIR" "$REPO/$(dirname "$VERIFY_SCRIPT")"
  printf 'readme\n' > "$REPO/README.md"
  printf 'sample directory\n' > "$REPO/$WRITE_SUBDIR/README.md"
  printf '#!/usr/bin/env bash\nexit 0\n' > "$REPO/$VERIFY_SCRIPT"
  # The daemon's prestart gate requires the verification script to be
  # executable (and shebang-lead); commit it with the exec bit set.
  chmod +x "$REPO/$VERIFY_SCRIPT"
  gitr add -A
  gitr commit -q -m "Initial commit"
  gitr push -q -u origin "$PROJECT_DEFAULT_BRANCH"
  # The remote's default branch, recorded the way a clone records it.
  gitr remote set-head origin "$PROJECT_DEFAULT_BRANCH"
}

write_worker() {
  # The stand-in worker. The shell backend runs it with the task text as $1 and
  # the worktree as $2.
  cat > "$S/bin/worker.sh" <<WORKER
#!/bin/sh
task="\$1"; dir="\$2"
tok="\$(printf '%s' "\$task" | grep -o 'LIVECHECK\[[a-z-]*:[a-z0-9]*\]' | head -1)"
mode="\$(printf '%s' "\$tok" | sed 's/^LIVECHECK\[\(.*\):.*\$/\1/')"
id="\$(printf '%s' "\$tok" | sed 's/^LIVECHECK\[.*:\(.*\)\]\$/\1/')"
case "\$task" in
  *.verify-output-*)
    # The daemon runs its verification as a one-shot session on the same
    # backend; the last line of that prompt is the file the verdict goes to.
    out="\$(printf '%s\n' "\$task" | tail -n 1)"
    printf '{"ok":true,"severity":"info","summary":"stand-in verifier of the live check; the task verification script is the evidence"}\n' > "\$out"
    exit 0 ;;
esac
[ -n "\$id" ] || exit 0
cd "\$dir" || exit 1
printf '%s %s\n' "\$id" "\$mode" >> "$STATE/runs.log"
case "\$mode" in
  work)
    # The session's own commit stages the write directory, so this file
    # becomes the one commit the branch carries that the default branch has
    # not merged.
    mkdir -p "$WRITE_SUBDIR"
    printf 'work of %s\n' "\$id" > "$WRITE_SUBDIR/\$id.txt"
    ;;
  nop)
    # Writes nothing: the session's commit has nothing to stage and makes no
    # commit, so the branch carries no work of its own.
    printf 'nop (no file written)\\n' > /dev/null
    ;;
esac
exit 0
WORKER
  chmod +x "$S/bin/worker.sh"
}

seed_store() {
  # Before the first start: two Automata awaiting review, one story each, one
  # task each, the write directory declared and the daemon left to make the
  # worktree when the run starts.
  mkdir -p "$DATA/autonomous"
  python3 -I - "$DATA/autonomous/prds.jsonl" "$WTROOT" "$WRITE_SUBDIR" "$VERIFY_SCRIPT" "$A_WORK" "$A_NOWORK" <<'PY'
import json, sys, datetime
out, wtroot, sub, verify = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
a_work, a_nowork = sys.argv[5], sys.argv[6]
now = datetime.datetime.now(datetime.timezone.utc).isoformat()
def rec(aid, name, mode):
    task = {
        "id": aid + "t1", "story_id": aid + "s1", "prd_id": aid,
        "title": "Sample task for " + name,
        "spec": "Sample task. LIVECHECK[%s:%s]" % (mode, aid),
        "status": "pending", "verification_script": verify,
        "created_at": now, "updated_at": now,
    }
    if mode == "work":
        # Declared the way the sibling live check declares it: the daemon's
        # own commit stages exactly the files the task declares.
        task["files"] = [sub + "/" + aid + ".txt"]
    return {
        "id": aid, "title": name,
        "spec": "Live check sample " + name + " LIVECHECK[%s:%s]" % (mode, aid),
        "project_dir": "", "status": "needs_review", "backend": "shell",
        "write_dirs": [wtroot + "/" + aid + "/" + sub],
        "stories": [{"id": aid + "s1", "prd_id": aid, "title": "Sample story", "status": "pending",
                     "tasks": [task], "created_at": now, "updated_at": now}],
        "created_at": now, "updated_at": now,
    }
with open(out, "w") as f:
    f.write(json.dumps(rec(a_work, "archive-work-branch", "work")) + "\n")
    f.write(json.dumps(rec(a_nowork, "archive-no-work-branch", "nop")) + "\n")
PY
}

write_config() {
  sed \
    -e "s|data_dir: /data|data_dir: $DATA|g" \
    -e "s|default_project_dir: /data/projects|default_project_dir: $PROJ|g" \
    -e "s|port: 8080|port: $PORT|g" \
    -e "s|tls_port: [0-9]*|tls_port: $TLS_PORT|g" \
    -e "s|sse_port: 9090|sse_port: $MCP_PORT|g" \
    -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
    -e "s|token: \"\"|token: \"$TOKEN\"|g" \
    -e "s|token: \"WEBHOOK_NOAUTH\"|token: \"\"|g" \
    -e "s|listen: \"127\.0\.0\.1:19053\".*|listen: \"127.0.0.1:$DNS_PORT\"|g" \
    -e "s|addr: 127\.0\.0\.1:19080|addr: 127.0.0.1:$HOOK_PORT|g" \
    -e "s|server_url: http://127\.0\.0\.1:19180|server_url: http://127.0.0.1:$NTFY_PORT|g" \
    "$ROOT/testdata/datawatch.yaml" > "$CFG"
  # The generated documentation artefacts are off: writing them saves the
  # Automaton between the roll-up's read and its commit, which aborts the
  # roll-up as stale and leaves the Automaton running although its task
  # completed. They are not what this check measures.
  # The worktree base repository, the worktree root, the allowed project root
  # and the stand-in worker, through the normal configuration keys.
  sed -i \
    -e "s|^autonomous:\$|autonomous:\n  verification_backend: shell\n  worktree_base_repo: $REPO\n  worktree_dir: $WTROOT\n  generated_artefacts:\n    change_record: false\n    gate_report: false\n    changelog_draft: false\n    parity_draft: false\n    tracker_rows: false\n    registry_row: false\n    combined_docs_task: false|" \
    -e "s|^server:\$|server:\n  allowed_project_roots: [\"$PROJ\", \"$WTROOT\"]|" \
    -e "s|^shell_backend:\$|shell_backend:\n  script_path: $S/bin/worker.sh|" \
    "$CFG"
}

# --- reads ---------------------------------------------------------------
cfg_get() { api GET /api/config | jq_r "$1"; }
cfg_grace() { cfg_get ".autonomous.supervisor_loop.archive_cancelled_after_minutes // empty"; }
prd() { api GET "/api/autonomous/prds/$1"; }
prd_status() { prd "$1" | jq_r '.status'; }
task_json() { prd "$1" | jq -c '[.stories[]?.tasks[]? | select(.status!=null)] | .[0] // {}'; }
branch_of() { prd "$1" | jq_r '.git.branch // empty'; }
# ahead_count <id>: how many commits of the branch are not on the default
# branch, read from the base repository. Empty when the branch ref is gone.
ahead_count() {
  local b; b="$(branch_of "$1")"
  [[ -n "$b" ]] || { printf 'ref-gone'; return; }
  gitr rev-list --count "$PROJECT_DEFAULT_BRANCH..refs/heads/$b" 2>/dev/null || printf 'ref-gone'
}
# no_branch_work <id>: true when the branch carries no unmerged work — either
# the ref is already gone (released: everything was on the base branch) or it
# is exactly in step with the default branch.
no_branch_work() {
  local c; c="$(ahead_count "$1")"
  [[ "$c" == "ref-gone" || "$c" == "0" ]]
}
cancel_prd() { api_code POST "/api/autonomous/prds/$1/cancel" '{}'; }
run_cfr() { api POST /api/supervisor-loop/jobs/close_finished_records/run; }
cfr_runs() { api GET "/api/supervisor-loop/runs?job=close_finished_records&limit=10"; }
last_cfr() { cfr_runs | jq_r '.runs[0] // {}'; }
decisions() { prd "$1" | jq_r '[.decisions[]? | "\(.kind):\(.actor // "")"] | join(" ")'; }
last_cancel_actor() { prd "$1" | jq_r '[.decisions[]? | select(.kind=="cancel")] | last | (.actor // "")'; }
last_archive_actor() { prd "$1" | jq_r '[.decisions[]? | select(.kind=="archive")] | last | (.actor // "")'; }
dlog() { cat "$LOG" "$DATA"/*.log 2>/dev/null; }
is_done() { [[ "$(prd_status "$1")" == completed ]]; }

diag() { # diag <id>: state of one Automaton and the log lines naming it
  say "  DIAG $1: Automaton $(prd_status "$1"); $(task_json "$1" | jq -c '{status, error, wait_reason}')"
  say "  DIAG decisions: $(prd "$1" | jq -c '[.decisions[]? | {kind, actor, note}]' | cut -c1-900)"
  dlog | grep -F "$1" | tail -8 | cut -c1-240 | sed 's/^/    log: /'
}

start_automaton() { # approve and run one Automaton through the REST API
  local code_a code_r
  code_a="$(api_code POST "/api/autonomous/prds/$1/approve" '{}')"
  code_r="$(api_code POST "/api/autonomous/prds/$1/run" '{}')"
  say "  approve $code_a, run $code_r ($1)"
  [[ "$code_a" == 200 && "$code_r" == 200 ]]
}

# ============================================================================
say "supervisor-loop archive live check — $(now) — binary: $BINARY ($VERSION)"
say "sandbox: $S"
say "ports: http $PORT tls $TLS_PORT mcp $MCP_PORT"

LEFT_SNAP="$S/leftover.snap"
env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" snapshot "$LEFT_SNAP" >/dev/null 2>&1 || true

CUR=0
build_project
write_worker
seed_store
write_config
BASE_REFS="$(gitr for-each-ref --format='%(refname:short) %(objectname:short)' refs/heads/)"
DEFAULT_PROBE="$(gitr symbolic-ref refs/remotes/origin/HEAD 2>&1); default branch: $PROJECT_DEFAULT_BRANCH (deliberately not main)"

# --- start the sandbox daemon ---------------------------------------------
export PATH="$S/bin:$PATH" TMPDIR="$DTMP" TMUX_TMPDIR="$SOCKDIR"
if ! td_start "$BINARY" "$CFG" "$LOG"; then
  say "FAIL: the sandbox daemon did not stay up: $(tail -5 "$LOG" 2>/dev/null)"
  exit 1
fi
DPID="$TD_LAST_PID"
td_set_api "$DPID" "$BASE" "$TOKEN"
if ! wait_until 90 curl -sk -m 3 -o /dev/null -f "$BASE/api/health"; then
  say "FAIL: the sandbox daemon does not answer /api/health: $(tail -5 "$LOG" 2>/dev/null)"
  exit 1
fi
say "  daemon pid $DPID healthy at $(now)"
DAEMON_UP_AT="$(now)"

# ============================================================================
begin_check 1 "Config: the grace setting round-trips"
# The key is unset in the generated config; the default reads 60.
D_GRACE="$(cfg_grace)"
say "  unset reads: ${D_GRACE:-unset} (the settings default for an unset key is 60)"
# Set it to 60 and read it back.
C60="$(api_both PUT /api/config "{\"$GRACE_KEY\":60}")"
C60_CODE="${C60%% *}"; C60_BODY="${C60#* }"
[[ "$C60_CODE" == 200 ]]; R1=$?
R60="$(cfg_grace)"
ck 1 $R1 "PUT $GRACE_KEY = 60 is accepted (200)" "response \`$C60_BODY\`"
[[ "$R60" == "60" ]]; ck 1 $? "the round-trip: GET reads the value back as 60" "stored value after the write: \`$R60\`; before the write: \`${D_GRACE:-unset}\`"
# A write below zero is refused and saves nothing.
CNEG="$(api_both PUT /api/config "{\"$GRACE_KEY\":-1}")"
CNEG_CODE="${CNEG%% *}"; CNEG_BODY="${CNEG#* }"
GNEG="$(cfg_grace)"
[[ "$CNEG_CODE" == 400 ]]; ck 1 $? "PUT $GRACE_KEY = -1 is refused (400)" "response code \`$CNEG_CODE\`, body: \`$CNEG_BODY\`"
[[ "$GNEG" == "60" ]]; ck 1 $? "the refused write saved nothing" "stored value after the refused write: \`$GNEG\`"

# ============================================================================
begin_check 2 "Grace 60: a cancelled Automaton with unmerged branch work survives the pass"
start_automaton "$A_WORK"; ck 2 $? "approve + run the work-carrying Automaton through the REST API" "Automaton \`$(prd_status "$A_WORK")\`"
if ! wait_until 180 is_done "$A_WORK"; then diag "$A_WORK"; fi
ST_W_DONE="$(prd_status "$A_WORK")"
[[ "$ST_W_DONE" == completed ]]; ck 2 $? "the work-carrying Automaton completes" "Automaton \`$ST_W_DONE\`, task \`$(task_json "$A_WORK" | jq -c '{status}')\`; worker runs: $(grep -c "^$A_WORK " "$STATE/runs.log" 2>/dev/null || true)"
AHEAD_W_BEFORE="$(ahead_count "$A_WORK")"
[[ "$AHEAD_W_BEFORE" =~ ^[1-9][0-9]*$ ]]; ck 2 $? "its branch carries a commit the default branch has not merged" "\`$(branch_of "$A_WORK")\` is $AHEAD_W_BEFORE commit(s) ahead of \`${PROJECT_DEFAULT_BRANCH}\`; worktree commit: $(wgit "$A_WORK" log --format='%h %s' -1 2>/dev/null)"
CXL_W="$(cancel_prd "$A_WORK")"
ST_W_CANCEL="$(prd_status "$A_WORK")"
[[ "$CXL_W" == 200 && "$ST_W_CANCEL" == "cancelled" ]]; ck 2 $? "the cancel returns 200 and leaves the Automaton cancelled" "status route \`$CXL_W\`; status \`$ST_W_CANCEL\`; cancel decision actor: \`$(last_cancel_actor "$A_WORK")\` (operator)"
# The supervisor-loop pass, with the grace at 60.
RUNS_BEFORE="$(cfr_runs | jq_r '.runs | length')"
OUT_W="$(run_cfr)"
OK_W="$(printf '%s' "$OUT_W" | jq_r '.ok')"; RES_W="$(printf '%s' "$OUT_W" | jq_r '.result')"
RUNS_AFTER="$(cfr_runs | jq_r '.runs | length')"
[[ "$OK_W" == true && "$RUNS_AFTER" -gt "$RUNS_BEFORE" ]]; ck 2 $? "the close_finished_records pass runs and is recorded" "run: ok=\`$OK_W\` result=\`$RES_W\`; run records for the job before/after: $RUNS_BEFORE/$RUNS_AFTER"
ST_W_AFTER="$(prd_status "$A_WORK")"
[[ "$ST_W_AFTER" == "cancelled" ]]; ck 2 $? "the cancelled Automaton survives the pass (not archived)" "status after the pass: \`$ST_W_AFTER\`; the pass reported it as held within the grace: \`$RES_W\`"
AHEAD_W_AFTER="$(ahead_count "$A_WORK")"
WT_W_ALIVE="no"; [[ -d "$(wt_of "$A_WORK")" ]] && WT_W_ALIVE="yes"
[[ "$AHEAD_W_AFTER" == "$AHEAD_W_BEFORE" && -n "$AHEAD_W_AFTER" ]]; ck 2 $? "the branch work is untouched (still $AHEAD_W_BEFORE commit(s) ahead)" "\`$(branch_of "$A_WORK")\` is $AHEAD_W_AFTER commit(s) ahead of \`$PROJECT_DEFAULT_BRANCH\`; worktree \`$(wt_of "$A_WORK"/)\` still present: $WT_W_ALIVE"
rep_block "The run record of the pass (the newest one):" "$(last_cfr | jq -c '{job, outcome, result}')"

# ============================================================================
begin_check 3 "Grace 0: a cancelled Automaton with no branch work is archived by the pass as supervisor-loop"
C0="$(api_both PUT /api/config "{\"$GRACE_KEY\":0}")"
C0_CODE="${C0%% *}"; C0_BODY="${C0#* }"
G0="$(cfg_grace)"
[[ "$C0_CODE" == 200 && "$G0" == "0" ]]; ck 3 $? "setting the grace to 0 round-trips (PUT 200, GET reads 0)" "response code \`$C0_CODE\`, body: \`$C0_BODY\`; stored value: \`$G0\`"
start_automaton "$A_NOWORK"; ck 3 $? "approve + run the no-work Automaton through the REST API" "Automaton \`$(prd_status "$A_NOWORK")\`"
if ! wait_until 180 is_done "$A_NOWORK"; then diag "$A_NOWORK"; fi
ST_N_DONE="$(prd_status "$A_NOWORK")"
[[ "$ST_N_DONE" == completed ]]; ck 3 $? "the no-work Automaton completes" "Automaton \`$ST_N_DONE\`; worker runs: $(grep -c "^$A_NOWORK " "$STATE/runs.log" 2>/dev/null || true) (the nop mode wrote no file)"
# Its branch carries no work: the ref is gone (released at completion because
# everything was on the base branch) or exactly in step with the default branch.
AHEAD_N="$(ahead_count "$A_NOWORK")"
no_branch_work "$A_NOWORK"; ck 3 $? "its branch carries no work of its own" "branch \`$(branch_of "$A_NOWORK")\`: ahead-of-default = \`$AHEAD_N\` (ref-gone means the daemon already released it, which is the same evidence)"
CXL_N="$(cancel_prd "$A_NOWORK")"
ST_N_CANCEL="$(prd_status "$A_NOWORK")"
[[ "$CXL_N" == 200 && "$ST_N_CANCEL" == "cancelled" ]]; ck 3 $? "the cancel returns 200 and leaves the Automaton cancelled" "status route \`$CXL_N\`; status \`$ST_N_CANCEL\`; cancel decision actor: \`$(last_cancel_actor "$A_NOWORK")\` (operator)"
OUT_N="$(run_cfr)"
OK_N="$(printf '%s' "$OUT_N" | jq_r '.ok')"; RES_N="$(printf '%s' "$OUT_N" | jq_r '.result')"
ST_N_AFTER="$(prd_status "$A_NOWORK")"
[[ "$OK_N" == true && "$ST_N_AFTER" == "archived" ]]; ck 3 $? "the pass archives it immediately (grace 0, no branch work)" "run: ok=\`$OK_N\` result=\`$RES_N\`; status after the pass: \`$ST_N_AFTER\`"
ARC_N="$(last_archive_actor "$A_NOWORK")"
[[ "$ARC_N" == "supervisor-loop" ]]; ck 3 $? "the archive decision is recorded under the supervisor-loop actor" "latest archive decision actor: \`$ARC_N\`; all decisions: \`$(decisions "$A_NOWORK")\`"
ST_W_G0="$(prd_status "$A_WORK")"
AHEAD_W_G0="$(ahead_count "$A_WORK")"
[[ "$ST_W_G0" == "cancelled" && "$AHEAD_W_G0" =~ ^[1-9][0-9]*$ ]]; ck 3 $? "the branch-carrying Automaton is still kept even at grace 0" "status of the first Automaton: \`$ST_W_G0\`; its branch is still $AHEAD_W_G0 commit(s) ahead; the pass reported: \`$RES_N\`"
rep_block "The run record of the pass (the newest one):" "$(last_cfr | jq -c '{job, outcome, result}')"

# ============================================================================
CUR=0
FINAL_REFS="$(gitr for-each-ref --format='%(refname:short)' refs/heads/ | tr '\n' ' ')"
DAEMON_LOG_TAIL="$(dlog | grep -E 'supervisor-loop|archive' | cut -c1-200 | tail -12)"

begin_check 4 "Cleanup"
td_stop_all
sleep 1
D_GONE="no"; td_alive "$DPID" || D_GONE="yes"
PORT_CLOSED="no"; td_wait_port_closed "$TLS_PORT" 20 && PORT_CLOSED="yes"
LEFT_OUT="$(env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" check "$LEFT_SNAP" 2>&1)"; LEFT_RC=$?
[[ "$D_GONE" == yes && "$PORT_CLOSED" == "yes" && "$LEFT_RC" == 0 ]]; ck 4 $? "the sandbox daemon and its sessions are gone and no test daemon was left behind" "daemon gone: $D_GONE (pid $DPID); port $TLS_PORT closed: $PORT_CLOSED; leftover check exit $LEFT_RC ($(printf '%s' "$LEFT_OUT" | tr '\n' ' ' | head -c 200))"
[[ -d "$(wt_of "$A_WORK")" ]]; ck 4 $? "the kept branch work survived the daemon ending (still readable in the scratch project)" "worktree \`$(wt_of "$A_WORK")\`; branch ahead: $(ahead_count "$A_WORK") (ref-gone is not expected here — the work was never merged)"
rep ""
rep "- The daemon was ended by \`td_stop_all\` from \`scripts/test-daemon-lib.sh\` (the recorded pid, config path as marker); nothing was ended by name."
rep "- The scratch area (project, remote, worktrees, data, config, log, records, worker, state) is removed by the exit handler right after this report is written."

# --- assemble the report ---------------------------------------------------
OVERALL=PASS; TOTAL_FAIL=0
for n in 1 2 3 4; do TOTAL_FAIL=$(( TOTAL_FAIL + ${CK_FAIL[$n]:-0} )); done
[[ $TOTAL_FAIL -gt 0 ]] && OVERALL=FAIL
# every numbered check must have run at least one assertion
for n in 1 2 3 4; do [[ "${CK_TOTAL[$n]:-0}" -gt 0 ]] || { OVERALL=FAIL; CK_FAIL[$n]=1; }; done

{
  printf '# Supervisor-loop archive grace — live check on a sandbox daemon\n\n'
  printf '**Date:** %s (run started %s, local time)\n' "$(date '+%Y-%m-%d')" "$DAEMON_UP_AT"
  printf '**Version under test:** %s (binary built from this worktree)\n' "${VERSION:-unknown}"
  printf '**Script:** `scripts/supervisor-loop-archive-live-check.sh` (run as `env -u TMUX -u TMUX_TMPDIR bash scripts/supervisor-loop-archive-live-check.sh`)\n'
  printf '**Scope:** the `autonomous.supervisor_loop.archive_cancelled_after_minutes` setting (round-trip and refusal), the grace holding a cancelled Automaton whose branch carries unmerged work, the immediate archive of a cancelled Automaton whose branch carries no work — with the `archive` decision recorded under the actor `supervisor-loop` — against a throwaway project repository whose default branch is `%s.\n\n' "$PROJECT_DEFAULT_BRANCH"
  printf '## Result\n\n**Overall:** %s\n\n' "$OVERALL"
  printf '| # | Check | Result | Assertions |\n|---|---|---|---|\n'
  for n in 1 2 3 4; do
    v=FAIL; [[ ${CK_FAIL[$n]:-0} -eq 0 ]] && v=PASS
    printf '| %s | %s | %s | %s of %s passed |\n' "$n" "${CK_TITLE[$n]}" "$v" "$(( ${CK_TOTAL[$n]:-0} - ${CK_FAIL[$n]:-0} ))" "${CK_TOTAL[$n]:-0}"
  done
  printf '\n## Setup\n\n'
  printf -- '- **Sandbox daemon only.** Started with `td_start` from `scripts/test-daemon-lib.sh` from a config generated from `testdata/datawatch.yaml` by the substitutions the test runner uses (data folder, ports, loopback host, test token). Added: `autonomous.worktree_base_repo` and `autonomous.worktree_dir` (the throwaway project and its worktree root), `server.allowed_project_roots`, and `shell_backend.script_path` (the stand-in worker). The production daemon, its service and its data were not touched, and nothing was ended by name.\n'
  printf -- '- **Ports.** HTTP %s, TLS %s, MCP %s, DNS %s, webhook %s, notify %s, each probed free just before use.\n' "$PORT" "$TLS_PORT" "$MCP_PORT" "$DNS_PORT" "$HOOK_PORT" "$NTFY_PORT"
  printf -- '- **Session sockets.** The daemon ran with its multiplexer socket directory under the scratch folder, so no session of the operator was reachable and none was touched. The script itself strips its own multiplexer variables and issues no multiplexer command.\n'
  printf -- '- **Everything** lives under one scratch folder inside the project'"'"'s `.datawatch` folder, removed on every exit path.\n'
  printf -- '- **Throwaway project.** A bare repository whose default branch is `%s` and which has no `main`; `refs/remotes/origin/HEAD` records it — the base the merge compare and the branch guard measure against.\n' "$PROJECT_DEFAULT_BRANCH"
  printf '\n```\n%s\n```\n\n' "$DEFAULT_PROBE"
  printf -- '- **Worker.** The daemon'"'"'s shell backend ran a generated script as the worker. The work-carrying Automaton'"'"'s task declares the write directory `%s/` and the worker writes one file there, which the session'"'"'s own commit stages and commits — that is the one commit the branch carries. The no-work Automaton'"'"'s task declares the same write directory but the worker writes nothing, and a session with nothing to stage never commits, so its branch stays in step with `%s. Every task carries a deterministic verification script and the daemon'"'"'s verification is answered by the same script with a fixed pass, so no model is involved anywhere.\n' "$WRITE_SUBDIR" "$PROJECT_DEFAULT_BRANCH"
  printf '\n| Automaton | Worker mode | Used in |\n|---|---|---|\n'
  printf '| `%s` archive-work-branch | writes its file under `%s/` (one commit the branch keeps) | checks 2, 3 (kept at both grace values) |\n' "$A_WORK" "$WRITE_SUBDIR"
  printf '| `%s` archive-no-work-branch | writes nothing (branch carries no work) | check 3 (archived at grace 0) |\n' "$A_NOWORK"
  printf '\n'
  cat "$S/rep-0.md" 2>/dev/null
  for n in 1 2 3 4; do cat "$S/rep-$n.md" 2>/dev/null; done
  printf '\n## Daemon log, the supervisor-loop lines\n\n```\n%s\n```\n\n' "$DAEMON_LOG_TAIL"
  printf '\n## Notes and what is not shown live\n\n'
  printf -- '- **How the two states were made.** The Automata were written into the sandbox store before its first start (the create route only makes drafts and a plan needs a planner model, which a sandbox does not have). Approve, run, both cancels, both settings and both manual job runs went through the REST API of the running daemon; the worktrees and branches were made by the daemon itself. The cancels are therefore recorded as the operator'"'"'s, and only the archive was recorded as `supervisor-loop` — the division the check is named after.\n'
  printf -- '- **The grace is a time floor, and the branch guard is absolute.** The pass archives a cancelled Automaton only when it has been cancelled for at least the grace AND its branch carries no unmerged work: unmerged branch work keeps the Automaton at any grace value, which is why the first Automaton is still `cancelled` after the grace-0 pass in check 3 while the second is `archived` in the same one.\n'
  printf -- '- **Not shown live:** the PWA and CLI reads of the setting and the run records (the REST reads are the ones this feature writes through), the idle-tick driven pass (the manual job run drives the identical step), and an archive after the grace elapsed naturally instead of being set to zero — the arithmetic is the same branch in the step, covered by its unit tests.\n'
  printf '\nResult: %s\n' "$OVERALL"
} > "$S/report.md"

SCRUB="$S/case_scrub.sed"
{
  printf 's|%s|<sandbox>|g\n' "$S"
  printf 's|%s|<worktree>|g\n' "$ROOT"
  printf 's|%s|<home>|g\n' "$HOME"
  printf 's|/[a-z0-9._-]*/\.datawatch/|/<path>/.datawatch/|g\n'
} > "$SCRUB"
sed -f "$SCRUB" "$S/report.md" > "$REPORT"

# --- final summary ----------------------------------------------------------
say ""
for n in 1 2 3 4; do
  v=FAIL; [[ ${CK_FAIL[$n]:-0} -eq 0 ]] && v=PASS
  say "$v  [$n] ${CK_TITLE[$n]} (${CK_TOTAL[$n]} assertion(s), ${CK_FAIL[$n]:-0} failed)"
done
say "Overall: $OVERALL — report: $REPORT"

if [[ "$OVERALL" == PASS ]]; then
  exit 0
fi
exit 1
