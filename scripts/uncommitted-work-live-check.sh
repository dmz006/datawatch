#!/usr/bin/env bash
# uncommitted-work-live-check.sh — live check of the Automaton uncommitted-work
# check on a SANDBOX daemon, against a throwaway project repository.
#
# What it proves (each result is a numbered check in the report):
#   1. Config: both settings round-trip through PUT and GET, an unknown word is
#      a 400 that names the allowed words, the defaults read hold and
#      task_files on a fresh config.
#   2. Read surfaces agree: GET /api/autonomous/uncommitted-work (200 shape, 405
#      for POST), the MCP tool autonomous_uncommitted_work, the CLI with --json
#      and the chat verb through POST /api/test/message return the same held list.
#   3. Held task: an Automaton in worktree mode whose task leaves a file
#      uncommitted is not completed: it is held, the reason lists the file, a
#      question with the options commit, discard and retry (default hold) exists,
#      and the hold shows in the Automaton JSON.
#   4. Answers: commit completes the task with a new post-verify commit and a
#      clean tree; discard removes only the listed files (a sibling survives);
#      retry runs the task again.
#   5. Whole-worktree block: with a stray untracked file in the worktree the
#      Automaton is not reported completed and is not in GET /api/autonomous/merge;
#      after the answer it completes.
#   6. Modes: warn completes with a recorded decision; off completes with no
#      decision and no commit attempt.
#   7. Cleanup: the sandbox daemon is gone and no test daemon was left behind.
#
# How the held state is made. The worker is the daemon's own shell backend
# pointed at a small generated script (shell_backend.script_path), so a worker
# runs headlessly and deterministically. Each Automaton declares one write
# directory (src/); the worker writes its sample file there, where the session's
# own commit picks it up, and, depending on the mode named in the task text,
# also drops a stray file at the worktree root, outside the write directory,
# which the session commit never stages. That is the state the check exists for:
# work left uncommitted in the worktree after the task finished.
#
# The Automata are written into the sandbox's own store before its first start:
# the create route only makes drafts and a plan needs a planner model, which a
# sandbox does not have. From the moment the daemon is up everything goes
# through its REST API (approve, run, answers, reads, settings). The worktrees
# are made by the daemon itself when a run starts (autonomous.worktree_base_repo).
#
# Generality: nothing here names this repository. The project is a throwaway
# repository created inside the sandbox whose default branch is NOT "main"
# (found by git from refs/remotes/origin/HEAD).
#
# Safety: SANDBOX only. The daemon runs from a generated config under a
# throwaway directory on loopback ports, with its own data directory, its own
# TMPDIR and its own multiplexer socket directory (TMUX_TMPDIR points under the
# throwaway directory, so no session of the operator is reachable). The
# production daemon, its service and its data are never touched. The daemon is
# ended only by the PID that scripts/test-daemon-lib.sh recorded, with the
# config path as marker. Everything this script made is removed on every exit
# path.
#
# Usage (always from a shell with the operator's multiplexer variables removed):
#   env -u TMUX -u TMUX_TMPDIR bash scripts/uncommitted-work-live-check.sh
#
# Env:
#   UNCOMMITTED_CHECK_REPORT  where the tracked report is written
#                             (default: docs/plans/2026-10-30-automation-uncommitted-work-live-check.md)
#   TEST_BINARY               daemon binary (default: bin/datawatch, built from
#                             this worktree when missing, then PATH)
#
# Exit 0 only when every check passed.
#
# The report is regenerated on every run, including its "Spec amendments and
# what is not shown live" section, so nothing in it depends on a hand edit.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/scripts/test-daemon-lib.sh"
REPORT="${UNCOMMITTED_CHECK_REPORT:-$ROOT/docs/plans/2026-10-30-automation-uncommitted-work-live-check.md}"

# The throwaway project's default branch: deliberately not "main".
PROJECT_DEFAULT_BRANCH="trunk"
# The directory every Automaton declares as its write scope.
WRITE_SUBDIR="src"
# The deterministic verification script every task carries (tracked in the project).
VERIFY_SCRIPT=".checks/ok.sh"

unset TMUX TMUX_TMPDIR

# --- scratch area and lifecycle ----------------------------------------------
S="$(mktemp -d "${TMPDIR:-/tmp}/dw-uncommitted-live.XXXXXX")"
export TD_STATE_DIR="$S/tdstate"
# shellcheck source=/dev/null
source "$LIB"

check_cleanup_extra() {
  # Runs AFTER td_stop_all (the daemon is gone): remove the scratch area. The
  # worktrees are plain directories of the throwaway repository.
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
WTROOT="$S/worktrees"       # the worktree root (autonomous.worktree_dir); one folder per Automaton id
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
  printf '%s %s' "${out##*$'\n'}" "$(printf '%s' "${out%$'\n'*}" | tr '\n' ' ')"
}
jq_r() { jq -r "$@" 2>/dev/null; }
dw() { env -u TMUX -u TMUX_TMPDIR TMUX_TMPDIR="$SOCKDIR" "$BINARY" --config "$CFG" "$@"; }

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

# --- the Automata ----------------------------------------------------------
# id|name|worker mode|extra (files declared by the task, or -)
#   modes: clean         writes its file under src/ only
#          stray         also drops a stray file at the worktree root, every run
#          stray-once    drops the stray file on its first run only
#          stray-fix     drops it on the first run, removes it on the second
#          declared      writes a file outside src/ that the task declares
AUTOMATA=(
  "d0c10001|held-commit|stray|-"
  "d0c10002|held-discard|stray-once|-"
  "d0c10003|held-retry|stray-fix|-"
  "d0c10004|mode-warn|stray|-"
  "d0c10005|mode-off|declared|notes/d0c10005.txt"
  "d0c10006|mode-hold-control|declared|notes/d0c10006.txt"
)
A_COMMIT=d0c10001; A_DISCARD=d0c10002; A_RETRY=d0c10003
A_WARN=d0c10004; A_OFF=d0c10005; A_CONTROL=d0c10006
tid() { printf '%st1' "$1"; }

build_project() {
  git init -q --bare -b "$PROJECT_DEFAULT_BRANCH" "$REMOTE"
  git clone -q "$REMOTE" "$REPO" 2>/dev/null
  gitr config user.email "uncommitted-live-check@example.invalid"
  gitr config user.name "uncommitted live check"
  gitr config commit.gpgsign false
  gitr checkout -q -b "$PROJECT_DEFAULT_BRANCH"
  mkdir -p "$REPO/$WRITE_SUBDIR" "$REPO/$(dirname "$VERIFY_SCRIPT")"
  printf 'readme\n' > "$REPO/README.md"
  printf 'sample directory\n' > "$REPO/$WRITE_SUBDIR/README.md"
  printf '#!/usr/bin/env bash\nexit 0\n' > "$REPO/$VERIFY_SCRIPT"
  gitr add -A
  gitr commit -q -m "Initial commit"
  gitr push -q -u origin "$PROJECT_DEFAULT_BRANCH"
  # The remote's default branch, recorded the way a clone records it.
  gitr remote set-head origin "$PROJECT_DEFAULT_BRANCH" >/dev/null
}

write_worker() {
  # The stand-in worker. The shell backend runs it with the task text as $1 and
  # the worktree as $2, then prints the completion marker itself.
  cat > "$S/bin/worker.sh" <<WORKER
#!/bin/sh
task="\$1"; dir="\$2"
tok="\$(printf '%s' "\$task" | grep -o 'LIVECHECK\[[a-z-]*:[a-z0-9]*\]' | head -1)"
mode="\$(printf '%s' "\$tok" | sed 's/^LIVECHECK\[\(.*\):.*\]\$/\1/')"
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
mkdir -p "$WRITE_SUBDIR"
printf 'work of %s\n' "\$id" > "$WRITE_SUBDIR/\$id.txt"
first=yes; [ -e "$STATE/\$id" ] && first=no
: > "$STATE/\$id"
case "\$mode" in
  stray) printf 'stray\n' > "stray-\$id.txt" ;;
  stray-once) [ "\$first" = yes ] && printf 'stray\n' > "stray-\$id.txt" ;;
  stray-fix) if [ "\$first" = yes ]; then printf 'stray\n' > "stray-\$id.txt"; else rm -f "stray-\$id.txt"; fi ;;
  declared) mkdir -p notes; printf 'declared\n' > "notes/\$id.txt" ;;
esac
exit 0
WORKER
  chmod +x "$S/bin/worker.sh"
}

seed_store() {
  # Before the first start: one Automaton per row, awaiting review, one story,
  # one task, the write directory declared and the daemon left to make the
  # worktree when the run starts.
  mkdir -p "$DATA/autonomous"
  python3 -I - "$DATA/autonomous/prds.jsonl" "$WTROOT" "$WRITE_SUBDIR" "$VERIFY_SCRIPT" "${AUTOMATA[@]}" <<'PY'
import json, sys, datetime
out, wtroot, sub, verify, rows = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5:]
now = datetime.datetime.now(datetime.timezone.utc).isoformat()
with open(out, "w") as f:
    for row in rows:
        aid, name, mode, files = row.split("|")
        task = {
            "id": aid + "t1", "story_id": aid + "s1", "prd_id": aid,
            "title": "Write the sample file for " + name,
            "spec": "Write the sample file. LIVECHECK[%s:%s]" % (mode, aid),
            "status": "pending", "verification_script": verify,
            "created_at": now, "updated_at": now,
        }
        if files != "-":
            task["files"] = [files]
        rec = {
            "id": aid, "title": name, "spec": "Live check sample " + name + " LIVECHECK[" + mode + ":" + aid + "]",
            "project_dir": "", "status": "needs_review", "backend": "shell",
            "write_dirs": [wtroot + "/" + aid + "/" + sub],
            "stories": [{"id": aid + "s1", "prd_id": aid, "title": "Sample story", "status": "pending",
                         "tasks": [task], "created_at": now, "updated_at": now}],
            "created_at": now, "updated_at": now,
        }
        f.write(json.dumps(rec) + "\n")
PY
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
  # The worktree base repository, the worktree root, the allowed project root
  # and the stand-in worker, through the normal configuration keys.
  sed -i \
    -e "s|^autonomous:\$|autonomous:\n  verification_backend: shell\n  questions:\n    enabled: true\n  worktree_base_repo: $REPO\n  worktree_dir: $WTROOT|" \
    -e "s|^server:\$|server:\n  allowed_project_roots: [\"$PROJ\", \"$WTROOT\"]|" \
    -e "s|^shell_backend:\$|shell_backend:\n  script_path: $S/bin/worker.sh|" \
    "$CFG"
}

# --- reads ---------------------------------------------------------------
cfg_get() { api GET /api/config | jq_r "$1"; }
uw_status() { api GET /api/autonomous/uncommitted-work; }
prd() { api GET "/api/autonomous/prds/$1"; }
prd_status() { prd "$1" | jq_r '.status'; }
task_json() { prd "$1" | jq -c --arg t "$(tid "$1")" '[.stories[]?.tasks[]? | select(.id==$t)][0] // {}'; }
task_field() { task_json "$1" | jq_r "$2"; }
hold_question() { task_field "$1" '.uncommitted_work.question_id // empty'; }
aud_count() { api GET "/api/audit?action=$1&limit=100" | jq_r --arg a "$1" '[.entries[]? | select(.details.prd_id=="'"$2"'")] | length'; }
decisions() { prd "$1" | jq_r '[.decisions[]?.kind] | join(" ")'; }
dlog() { cat "$LOG" "$DATA"/*.log 2>/dev/null; }

# Conditions for wait_until.
is_held() { [[ "$(task_field "$1" '.status')" == blocked && "$(task_field "$1" '.uncommitted_work.files | length')" -ge 1 && -n "$(hold_question "$1")" ]]; }
is_done() { [[ "$(prd_status "$1")" == completed ]]; }
task_done() { [[ "$(task_field "$1" '.status')" == completed ]]; }
rollup_held() { [[ "$(prd_status "$1")" == blocked && "$(prd "$1" | jq_r '.uncommitted_work.files | length')" -ge 1 ]]; }
merge_lists() { api GET /api/autonomous/merge | jq -e --arg a "$1" '[.automata[]? | select(.automaton_id==$a)] | length > 0' >/dev/null 2>&1; }
# real_dirty: what git reports in a worktree apart from the files the daemon
# itself writes there for a session (its MCP discovery file, the ignore-file
# lines it adds and its injected skills). Those stay untracked and are not
# counted by the check (see check 3).
real_dirty() { wgit "$1" status --porcelain | grep -vE ' (\.mcp\.json|\.gitignore|\.datawatch/)' || true; }
worker_runs() { grep -c "^$1 " "$STATE/runs.log" 2>/dev/null || true; }

diag() { # diag <id>: state of one Automaton and the log lines naming it, to standard output only
  say "  DIAG $1: Automaton $(prd_status "$1"); task $(task_json "$1" | jq -c '{status, error, wait_reason, failure_reason}')"
  dlog | grep -F "$1" | tail -8 | cut -c1-240 | sed 's/^/    log: /'
}

start_automaton() { # approve and run one Automaton through the REST API
  local code_a code_r
  code_a="$(api_code POST "/api/autonomous/prds/$1/approve" '{}')"
  code_r="$(api_code POST "/api/autonomous/prds/$1/run" '{}')"
  say "  approve $code_a, run $code_r ($1)"
  [[ "$code_a" == 200 && "$code_r" == 200 ]]
}
answer() { # answer <question id> <option id> -> HTTP code
  api_code POST "/api/questions/$1/answer" "{\"chosen_option_id\":\"$2\",\"actor\":\"operator\",\"surface\":\"cli\"}"
}

# ============================================================================
say "uncommitted-work live check — $(now) — binary: $BINARY ($VERSION)"
say "ports: http $PORT tls $TLS_PORT mcp $MCP_PORT"

LEFT_SNAP="$S/leftover.snap"
env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" snapshot "$LEFT_SNAP" >/dev/null 2>&1 || true

CUR=0
build_project
write_worker
seed_store
write_config
BASE_REFS="$(gitr for-each-ref --format='%(refname:short) %(objectname:short)' refs/heads/)"
DEFAULT_PROBE="$(gitr symbolic-ref refs/remotes/origin/HEAD 2>&1); main exists: $(gitr show-ref --verify --quiet refs/heads/main && echo yes || echo no)"

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
begin_check 1 "Config: the two settings"
POL="autonomous.automation.uncommitted_work_policy"; SCO="autonomous.automation.uncommitted_work_scope"
# defaults on a fresh config
D_ST="$(uw_status)"
D_POL_API="$(printf '%s' "$D_ST" | jq_r '.policy')"; D_SCO_API="$(printf '%s' "$D_ST" | jq_r '.scope')"
D_POL_CFG="$(cfg_get '.autonomous.automation.uncommitted_work_policy // ""')"; D_SCO_CFG="$(cfg_get '.autonomous.automation.uncommitted_work_scope // ""')"
[[ "$D_POL_API" == hold && "$D_SCO_API" == task_files ]]; ck 1 $? "a fresh config reads policy hold and scope task_files" "status route: policy=\`$D_POL_API\` scope=\`$D_SCO_API\`; raw settings before any write: policy=\`${D_POL_CFG:-unset}\` scope=\`${D_SCO_CFG:-unset}\` (unset reads as the default)"
# round trip, PUT /api/config
RT_OK=0; RT_TXT=""
for w in warn off hold; do
  code="$(api_code PUT /api/config "{\"$POL\":\"$w\"}")"; got="$(cfg_get '.autonomous.automation.uncommitted_work_policy')"; eff="$(uw_status | jq_r '.policy')"
  RT_TXT+="$w: HTTP $code, read \`$got\`, status \`$eff\`; "
  [[ "$code" == 200 && "$got" == "$w" && "$eff" == "$w" ]] || RT_OK=1
done
for w in worktree task_files; do
  code="$(api_code PUT /api/config "{\"$SCO\":\"$w\"}")"; got="$(cfg_get '.autonomous.automation.uncommitted_work_scope')"; eff="$(uw_status | jq_r '.scope')"
  RT_TXT+="$w: HTTP $code, read \`$got\`, status \`$eff\`; "
  [[ "$code" == 200 && "$got" == "$w" && "$eff" == "$w" ]] || RT_OK=1
done
ck 1 $RT_OK "PUT then GET round-trips the policy (warn, off, hold) and the scope (worktree, task_files)" "$RT_TXT"
# the Automata settings route
code="$(api_code PUT /api/autonomous/config '{"automation":{"uncommitted_work_policy":"warn","uncommitted_work_scope":"worktree"}}')"
g1="$(api GET /api/autonomous/config | jq_r '.automation.uncommitted_work_policy')"; g2="$(api GET /api/autonomous/config | jq_r '.automation.uncommitted_work_scope')"
code2="$(api_code PUT /api/autonomous/config '{"automation":{"uncommitted_work_policy":"hold","uncommitted_work_scope":"task_files"}}')"
[[ "$code" == 200 && "$g1" == warn && "$g2" == worktree && "$code2" == 200 ]]; ck 1 $? "the Automata settings route round-trips both settings too" "PUT HTTP $code read policy \`$g1\` scope \`$g2\`; restored with HTTP $code2"
# unknown words
B_POL="$(api_both PUT /api/config "{\"$POL\":\"sometimes\"}")"; B_SCO="$(api_both PUT /api/config "{\"$SCO\":\"everywhere\"}")"
B_POL2="$(api_both PUT /api/autonomous/config '{"automation":{"uncommitted_work_policy":"sometimes"}}')"; B_SCO2="$(api_both PUT /api/autonomous/config '{"automation":{"uncommitted_work_scope":"everywhere"}}')"
AFTER_POL="$(uw_status | jq_r '.policy')"; AFTER_SCO="$(uw_status | jq_r '.scope')"
[[ "$B_POL" == 400* && "$B_POL" == *hold* && "$B_POL" == *warn* && "$B_POL" == *off* && "$B_POL2" == 400* && "$B_POL2" == *hold* && "$B_POL2" == *warn* && "$B_POL2" == *off* ]]
ck 1 $? "an unknown policy word is a 400 that names hold, warn and off (both routes)" "\`PUT /api/config\`: $B_POL — settings route: $B_POL2"
[[ "$B_SCO" == 400* && "$B_SCO" == *task_files* && "$B_SCO" == *worktree* && "$B_SCO2" == 400* && "$B_SCO2" == *task_files* && "$B_SCO2" == *worktree* ]]
ck 1 $? "an unknown scope word is a 400 that names task_files and worktree (both routes)" "\`PUT /api/config\`: $B_SCO — settings route: $B_SCO2"
[[ "$AFTER_POL" == hold && "$AFTER_SCO" == task_files ]]; ck 1 $? "the refused writes saved nothing" "policy \`$AFTER_POL\`, scope \`$AFTER_SCO\` after the four refused writes"

# ============================================================================
# Setup for checks 2 to 5: three Automata run until each task is held.
CUR=0
for id in "$A_COMMIT" "$A_DISCARD" "$A_RETRY"; do start_automaton "$id" || say "  WARN: $id did not start"; done
HELD_OK=0
declare -A HELD_AT=()
HELD_DEADLINE=$((SECONDS + 300))
while ((SECONDS < HELD_DEADLINE)); do
  ALL=1
  for id in "$A_COMMIT" "$A_DISCARD" "$A_RETRY"; do
    if [[ -z "${HELD_AT[$id]:-}" ]]; then
      if is_held "$id"; then HELD_AT[$id]="$(now)"; say "  $id held at ${HELD_AT[$id]}"; else ALL=0; fi
    fi
  done
  [[ $ALL == 1 ]] && break
  sleep 2
done
for id in "$A_COMMIT" "$A_DISCARD" "$A_RETRY"; do
  [[ -n "${HELD_AT[$id]:-}" ]] || { HELD_OK=1; diag "$id"; }
done
say "  holds placed at $(now): commit=$(task_field "$A_COMMIT" '.status') discard=$(task_field "$A_DISCARD" '.status') retry=$(task_field "$A_RETRY" '.status')"

begin_check 3 "Held task: a task that leaves a file uncommitted is not completed"
rep ""
rep "Approach used: the **shell backend with a generated worker script** ran the task headlessly in the worktree the daemon made (no staging by hand was needed). The Automaton declares one write directory (\`$WRITE_SUBDIR/\`); the worker writes its sample file there, which the session's own commit stages, and also drops \`stray-<id>.txt\` at the worktree root, outside the write directory, which that commit never stages."
for id in "$A_COMMIT"; do
  T="$(task_json "$id")"; Q="$(hold_question "$id")"
  rep_block "Task JSON of the held task (\`uncommitted_work\` is the hold):" "$(printf '%s' "$T" | jq '{id, status, error, uncommitted_work}')"
  [[ "$(printf '%s' "$T" | jq_r '.status')" == blocked && "$(task_field "$id" '.completed_at // "none"')" == none ]]
  ck 3 $? "the task is held (blocked), not completed" "status \`$(printf '%s' "$T" | jq_r '.status')\`, error \`$(printf '%s' "$T" | jq_r '.error')\`"
  printf '%s' "$T" | jq -e --arg f "stray-$id.txt" '.uncommitted_work.files | index($f) != null' >/dev/null 2>&1
  ck 3 $? "the hold lists the stray file" "files $(printf '%s' "$T" | jq -c '.uncommitted_work.files'), outside the declared files $(printf '%s' "$T" | jq -c '.uncommitted_work.outside_scope'), reason \`$(printf '%s' "$T" | jq_r '.uncommitted_work.reason')\`"
  [[ "$(printf '%s' "$T" | jq -c '.uncommitted_work.files')" == "[\"stray-$id.txt\"]" ]]
  ck 3 $? "only the stray file is listed: the files the daemon itself wrote into the worktree (session config, injected skills, ignore-file lines) are not counted" "worktree status at that moment: \`$(wgit "$id" status --porcelain | tr '\n' ' ')\`"
  QJ="$(api GET "/api/questions/$Q")"
  rep_block "The question (\`GET /api/questions/<id>\`, the \`question\` record):" "$(printf '%s' "$QJ" | jq '.question | {id, label, state, default, context, options: [.options[]? | .id], text}' 2>/dev/null || printf '%s' "$QJ")"
  printf '%s' "$QJ" | jq -e '[.question.options[].id] | (index("commit") != null and index("discard") != null and index("retry") != null)' >/dev/null 2>&1; o1=$?
  [[ "$(printf '%s' "$QJ" | jq_r '.question.default')" == hold && "$(printf '%s' "$QJ" | jq_r '.question.state')" == open && "$(printf '%s' "$QJ" | jq_r '.question.label')" == uncommitted_work ]]; o2=$?
  ck 3 $((o1 + o2)) "an open question offers commit, discard and retry with the default hold" "question \`$Q\`: options $(printf '%s' "$QJ" | jq -c '[.question.options[]?.id]'), default \`$(printf '%s' "$QJ" | jq_r '.question.default')\`, state \`$(printf '%s' "$QJ" | jq_r '.question.state')\`"
  [[ "$(wgit "$id" status --porcelain | grep -c "stray-$id.txt")" == 1 ]]
  ck 3 $? "git agrees: the stray file is untracked in the worktree" "$(wgit "$id" status --porcelain | tr '\n' ' ')"
  prd "$id" | jq -e --arg t "$(tid "$id")" '[.stories[].tasks[] | select(.id==$t) | .uncommitted_work.question_id] | length == 1' >/dev/null 2>&1
  ck 3 $? "the hold shows in the Automaton JSON, on the task" "\`GET\` of the Automaton: stories[0].tasks[0].uncommitted_work present"
  [[ "$(prd_status "$id")" != completed ]]; ck 3 $? "the Automaton is not completed" "status \`$(prd_status "$id")\`"
  AUDH="$(aud_count autonomous_uncommitted_work_held "$id")"; DEC="$(decisions "$id")"
  [[ "$AUDH" -ge 1 && "$DEC" == *uncommitted_work_held* ]]; ck 3 $? "the hold is in the audit log and the decision log" "audit events $AUDH; decisions \`$DEC\`"
done
for id in "$A_DISCARD" "$A_RETRY"; do
  is_held "$id"; ck 3 $? "the same hold is placed on the other two Automata (\`$id\`)" "task status \`$(task_field "$id" '.status')\`, files $(task_field "$id" '.uncommitted_work.files | tostring')"
done
[[ $HELD_OK == 0 ]] || say "  WARN: not every hold appeared in time"

# ============================================================================
begin_check 2 "Read surfaces agree"
SURF_PY="$S/surfaces.py"
cat > "$SURF_PY" <<'PY'
import json, sys
rest, cli_json, mcp_text, chat_text = [open(p).read() for p in sys.argv[1:5]]
problems = []
try:
    r = json.loads(rest); c = json.loads(cli_json); m = json.loads(mcp_text)
except Exception as e:
    print("a read did not return the status: REST %r, CLI %r, MCP %r" % (rest[:60].strip(), cli_json[:60].strip(), mcp_text[:60].strip())); sys.exit(1)
canon = lambda d: json.dumps(d, sort_keys=True)
if canon(r) != canon(c): problems.append("CLI --json differs from REST")
if canon(r) != canon(m): problems.append("MCP result differs from REST")
if not r.get("held"): problems.append("REST holds nothing")
if "policy: %s, scope: %s" % (r["policy"], r["scope"]) not in chat_text: problems.append("chat reply misses the policy and scope line")
for e in r.get("held", []):
    if e["automaton_id"] not in chat_text: problems.append("chat reply misses " + e["automaton_id"])
    for f in e["files"]:
        if f not in chat_text: problems.append("chat reply misses file " + f)
    if e.get("question_id") and e["question_id"] not in chat_text: problems.append("chat reply misses question " + e["question_id"])
if ("%d held" % len(r.get("held", []))) not in chat_text: problems.append("chat reply misses the held count")
print("OK" if not problems else "; ".join(problems))
sys.exit(0 if not problems else 1)
PY
mcp_read() { # prints the autonomous_uncommitted_work result text
  env -u TMUX -u TMUX_TMPDIR TMUX_TMPDIR="$SOCKDIR" python3 -I - "$BINARY" "$CFG" <<'PY'
import json, subprocess, sys, os, select, time
bin_, cfg = sys.argv[1], sys.argv[2]
p = subprocess.Popen([bin_, "--config", cfg, "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                     stderr=subprocess.DEVNULL, env=dict(os.environ), text=True)
def send(o): p.stdin.write(json.dumps(o) + "\n"); p.stdin.flush()
send({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2024-11-05", "capabilities": {}, "clientInfo": {"name": "uncommitted-live-check", "version": "0"}}})
send({"jsonrpc": "2.0", "method": "notifications/initialized"})
send({"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": "autonomous_uncommitted_work", "arguments": {}}})
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
REST_CODE="$(api_code GET /api/autonomous/uncommitted-work)"; POST_CODE="$(api_code POST /api/autonomous/uncommitted-work '{}')"
SHAPE_OK=1; uw_status | jq -e '(.policy|type=="string") and (.scope|type=="string") and (.held|type=="array") and all(.held[]; has("automaton_id") and has("name") and has("kind") and (.files|type=="array") and (.outside_scope|type=="array") and has("question_id") and has("held_at"))' >/dev/null 2>&1 && SHAPE_OK=0
[[ "$REST_CODE" == 200 ]]; ck 2 $? "\`GET /api/autonomous/uncommitted-work\` answers 200" "HTTP $REST_CODE"
ck 2 $SHAPE_OK "the body has the policy, the scope and a held list whose entries carry the Automaton, kind, files, outside-scope files, question and time" "$(uw_status | jq -c '{policy, scope, held: [.held[] | {automaton_id, task_id, kind, files}]}')"
[[ "$POST_CODE" == 405 ]]; ck 2 $? "POST is a 405" "HTTP $POST_CODE"
SV=""
for try in 1 2 3; do
  api GET /api/autonomous/uncommitted-work > "$S/s-rest.json"
  dw autonomous uncommitted-work --json > "$S/s-cli.json" 2> "$S/s-cli.err"
  mcp_read > "$S/s-mcp.json"
  api POST /api/test/message '{"text":"autonomous uncommitted-work"}' | jq -r '.responses | join("\n")' > "$S/s-chat.txt" 2>/dev/null
  SV="$(python3 -I "$SURF_PY" "$S/s-rest.json" "$S/s-cli.json" "$S/s-mcp.json" "$S/s-chat.txt" 2>&1)"
  [[ "$SV" == OK ]] && break
  sleep 2
done
rep_block "CLI text (\`datawatch autonomous uncommitted-work\`):" "$(dw autonomous uncommitted-work 2>&1)"
rep_block "Chat reply (\`POST /api/test/message\`, text \`autonomous uncommitted-work\`):" "$(cat "$S/s-chat.txt")"
rep_block "MCP result (\`autonomous_uncommitted_work\`, first 700 bytes):" "$(head -c 700 "$S/s-mcp.json")"
[[ "$SV" == OK ]]; ck 2 $? "REST, MCP, CLI (--json) and the chat verb return the same held list" "comparison: \`$SV\`; held entries: $(jq -c '[.held[] | {automaton_id, task_id, kind}]' "$S/s-rest.json" 2>/dev/null)"
N_FILTER="$(api GET "/api/autonomous/uncommitted-work?automaton=$A_COMMIT" | jq_r '.held | length')"; N_NONE="$(api GET "/api/autonomous/uncommitted-work?automaton=nosuch" | jq_r '.held | length')"
[[ "$N_FILTER" == 1 && "$N_NONE" == 0 ]]; ck 2 $? "the automaton filter narrows the list (an unknown id reads empty)" "\`?automaton=$A_COMMIT\` holds $N_FILTER, an unknown id holds $N_NONE"

# ============================================================================
begin_check 4 "Answers: commit, discard and retry"
# --- commit ---
Q="$(hold_question "$A_COMMIT")"; PRE_HEAD="$(wgit "$A_COMMIT" rev-parse HEAD)"; PRE_PV="$(task_field "$A_COMMIT" '.post_verify_sha // ""')"
CODE="$(answer "$Q" commit)"
wait_until 90 task_done "$A_COMMIT"; TD=$?; [[ $TD == 0 ]] || diag "$A_COMMIT"
wait_until 90 is_done "$A_COMMIT"
POST_HEAD="$(wgit "$A_COMMIT" rev-parse HEAD)"; PV="$(task_field "$A_COMMIT" '.post_verify_sha // ""')"
CLEAN="$(real_dirty "$A_COMMIT" | tr '\n' ' ')"
[[ "$CODE" == 2?? && $TD == 0 ]]; ck 4 $? "commit: the answer is accepted and the task completes" "answer HTTP $CODE; task status \`$(task_field "$A_COMMIT" '.status')\`"
[[ -n "$PV" && "$PV" == "$POST_HEAD" && "$PV" != "$PRE_HEAD" && -z "$PRE_PV" ]]; ck 4 $? "commit: the task completes with a new post-verify commit (the worktree head)" "head before the answer \`${PRE_HEAD:0:9}\`, post_verify_sha now \`${PV:0:9}\`, head now \`${POST_HEAD:0:9}\`"
[[ -z "$CLEAN" ]]; ck 4 $? "commit: the worktree is clean (apart from the daemon's own session files)" "git status without those files: \`${CLEAN:-clean}\`; with them: \`$(wgit "$A_COMMIT" status --porcelain | tr '\n' ' ')\`"
wgit "$A_COMMIT" show --name-only --format=%s HEAD | grep -q "stray-$A_COMMIT.txt"; ck 4 $? "commit: the listed file is in the new commit on the Automaton branch" "$(wgit "$A_COMMIT" show --name-only --format='%s' HEAD | tr '\n' ' ')"
# --- discard ---
Q="$(hold_question "$A_DISCARD")"
printf 'sibling\n' > "$(wt_of "$A_DISCARD")/sibling-$A_DISCARD.txt"
rep_block "Worktree of the discard case just before the answer (a sibling file was added after the hold was placed):" "$(wgit "$A_DISCARD" status --porcelain)"
CODE="$(answer "$Q" discard)"
wait_until 90 task_done "$A_DISCARD"; TD=$?; [[ $TD == 0 ]] || diag "$A_DISCARD"
STRAY_GONE=1; [[ ! -e "$(wt_of "$A_DISCARD")/stray-$A_DISCARD.txt" ]] && STRAY_GONE=0
SIB_KEPT=1; [[ -f "$(wt_of "$A_DISCARD")/sibling-$A_DISCARD.txt" ]] && SIB_KEPT=0
[[ "$CODE" == 2?? ]]; ck 4 $? "discard: the answer is accepted" "answer HTTP $CODE"
ck 4 $STRAY_GONE "discard: the listed file is gone from the worktree" "stray file present: $([[ $STRAY_GONE == 0 ]] && echo no || echo yes)"
ck 4 $SIB_KEPT "discard: a sibling file that was not listed survives" "sibling present: $([[ $SIB_KEPT == 0 ]] && echo yes || echo no)"
RUNS="$(worker_runs "$A_DISCARD")"
[[ $TD == 0 && "$RUNS" == 2 ]]; ck 4 $? "discard: the task ran again on the clean tree and completed" "worker runs $RUNS; task status \`$(task_field "$A_DISCARD" '.status')\`"
# --- retry ---
Q="$(hold_question "$A_RETRY")"; R_BEFORE="$(worker_runs "$A_RETRY")"; R_FILES_BEFORE="$(wgit "$A_RETRY" status --porcelain | tr '\n' ' ')"
CODE="$(answer "$Q" retry)"
wait_until 90 task_done "$A_RETRY"; TD=$?; [[ $TD == 0 ]] || diag "$A_RETRY"
wait_until 90 is_done "$A_RETRY"; AD=$?; [[ $AD == 0 ]] || diag "$A_RETRY"
R_AFTER="$(worker_runs "$A_RETRY")"
[[ "$CODE" == 2?? && $TD == 0 && "$R_BEFORE" == 1 && "$R_AFTER" == 2 ]]; ck 4 $? "retry: the task ran again (the worker ran a second time) and completed" "answer HTTP $CODE; worker runs $R_BEFORE then $R_AFTER; worktree before the answer: \`$R_FILES_BEFORE\`; task status \`$(task_field "$A_RETRY" '.status')\`"
[[ $AD == 0 && -z "$(real_dirty "$A_RETRY")" ]]; ck 4 $? "retry: the Automaton then completed on a clean tree" "Automaton status \`$(prd_status "$A_RETRY")\`; git status without the daemon's own session files \`$(real_dirty "$A_RETRY" | tr '\n' ' ')\`"
RES_AUD="$(aud_count autonomous_uncommitted_work_resolved "$A_COMMIT")$(aud_count autonomous_uncommitted_work_resolved "$A_DISCARD")$(aud_count autonomous_uncommitted_work_resolved "$A_RETRY")"
[[ "$RES_AUD" =~ ^[1-9]+$ ]] && [[ "$RES_AUD" != *0* ]]; ck 4 $? "each answer is in the audit log" "resolved events per case (commit, discard, retry): $RES_AUD"

# ============================================================================
begin_check 5 "Whole-worktree block"
rep ""
rep "The discard case from check 4 is the stray-file case: its sibling file is a stray untracked file that no task owns, left in the worktree when the task finished (the same state a cancel or a backend move leaves). Its task completed in check 4; the Automaton must now be held at the finished check."
api PUT /api/config '{"autonomous.automation.merge_pending_mode":"shadow"}' >/dev/null
wait_until 90 rollup_held "$A_DISCARD"; RH=$?; [[ $RH == 0 ]] || diag "$A_DISCARD"
ST="$(prd_status "$A_DISCARD")"; HOLD="$(prd "$A_DISCARD" | jq -c '.uncommitted_work // {}')"
rep_block "Automaton JSON, the hold on the whole worktree (\`uncommitted_work\`) and the blocked reason:" "$(prd "$A_DISCARD" | jq '{status, blocked_reason, uncommitted_work}' 2>/dev/null)"
[[ $RH == 0 && "$ST" != completed ]]; ck 5 $? "with a stray untracked file in the worktree the Automaton is not reported completed" "status \`$ST\`; hold kind \`$(printf '%s' "$HOLD" | jq_r '.kind')\`, files $(printf '%s' "$HOLD" | jq -c '.files')"
sleep 5
MERGE_BODY="$(api GET /api/autonomous/merge)"
rep_block "\`GET /api/autonomous/merge\` while the Automaton is held (mode shadow lists completed Automata with a branch):" "$(printf '%s' "$MERGE_BODY" | jq -c '{mode, automata: [.automata[]? | {automaton_id, branch, state}]}')"
! merge_lists "$A_DISCARD"; ck 5 $? "the held Automaton is not in the merge list" "listed: $(printf '%s' "$MERGE_BODY" | jq -c '[.automata[]?.automaton_id]')"
merge_lists "$A_COMMIT" || wait_until 90 merge_lists "$A_COMMIT"; ck 5 $? "a completed Automaton on a clean tree is in the merge list (the list works, so the absence above is the block)" "listed: $(api GET /api/autonomous/merge | jq -c '[.automata[]?.automaton_id]')"
Q="$(prd "$A_DISCARD" | jq_r '.uncommitted_work.question_id // empty')"
[[ "$(api GET "/api/questions/$Q" | jq_r '.question.options | map(.id) | join(",")')" == "commit,discard,retry" ]]; ck 5 $? "a question for the whole worktree is open with the same three options" "question \`$Q\`"
CODE="$(answer "$Q" commit)"
wait_until 90 is_done "$A_DISCARD"; AD=$?; [[ $AD == 0 ]] || diag "$A_DISCARD"
[[ "$CODE" == 2?? && $AD == 0 ]]; ck 5 $? "after the answer (commit) the Automaton completes" "answer HTTP $CODE; status \`$(prd_status "$A_DISCARD")\`"
[[ -z "$(real_dirty "$A_DISCARD")" && -z "$(prd "$A_DISCARD" | jq_r '.uncommitted_work // empty')" ]]; ck 5 $? "the tree is clean and the hold is cleared" "git status without the daemon's own session files \`$(real_dirty "$A_DISCARD" | tr '\n' ' ')\`"
wait_until 90 merge_lists "$A_DISCARD"; ck 5 $? "once completed it is listed as merge ready" "listed: $(api GET /api/autonomous/merge | jq -c '[.automata[]?.automaton_id]')"
[[ "$(uw_status | jq_r '.held | length')" == 0 ]]; ck 5 $? "the held list is empty again" "$(uw_status | jq -c '.held')"

# ============================================================================
begin_check 6 "Modes: warn and off"
# control: policy hold, an in-scope leftover is committed by the executor itself
start_automaton "$A_CONTROL" >/dev/null
wait_until 120 is_done "$A_CONTROL"; CD=$?; [[ $CD == 0 ]] || diag "$A_CONTROL"
C_LOG="$(wgit "$A_CONTROL" log --format=%s -3 | tr '\n' '|')"
CTRL_COMMITTED=1; [[ -z "$(real_dirty "$A_CONTROL")" ]] && wgit "$A_CONTROL" ls-files --error-unmatch "notes/$A_CONTROL.txt" >/dev/null 2>&1 && CTRL_COMMITTED=0
ck 6 $((CD + CTRL_COMMITTED)) "control (policy hold): a file the task declared but the session did not commit is committed by the executor and the Automaton completes" "Automaton status \`$(prd_status "$A_CONTROL")\`; recent commits \`$C_LOG\`; git status \`$(wgit "$A_CONTROL" status --porcelain | tr '\n' ' ')\`"
# warn
api PUT /api/config "{\"$POL\":\"warn\"}" >/dev/null
start_automaton "$A_WARN" >/dev/null
wait_until 120 is_done "$A_WARN"; WD=$?; [[ $WD == 0 ]] || diag "$A_WARN"
W_DEC="$(decisions "$A_WARN")"; W_AUD="$(aud_count autonomous_uncommitted_work_warned "$A_WARN")"
W_HELD="$(uw_status | jq_r '.held | length')"
rep_block "Decision log of the warn case:" "$(prd "$A_WARN" | jq -c '[.decisions[]? | select(.kind | startswith("uncommitted_work")) | {kind, note}]')"
[[ $WD == 0 && "$(task_field "$A_WARN" '.status')" == completed ]]; ck 6 $? "warn: the task and the Automaton complete although the stray file is uncommitted" "Automaton \`$(prd_status "$A_WARN")\`, task \`$(task_field "$A_WARN" '.status')\`; git status \`$(wgit "$A_WARN" status --porcelain | tr '\n' ' ')\`"
[[ "$W_DEC" == *uncommitted_work_warned* && "$W_AUD" -ge 1 && "$W_HELD" == 0 ]]; ck 6 $? "warn: a decision and an audit event are recorded, no hold and no question" "decisions \`$W_DEC\`; audit events $W_AUD; held entries $W_HELD"
# off
api PUT /api/config "{\"$POL\":\"off\"}" >/dev/null
OFF_HEAD_BEFORE=""
start_automaton "$A_OFF" >/dev/null
wait_until 120 is_done "$A_OFF"; OD=$?; [[ $OD == 0 ]] || diag "$A_OFF"
O_DEC="$(decisions "$A_OFF")"; O_AUD=$(( $(aud_count autonomous_uncommitted_work_held "$A_OFF") + $(aud_count autonomous_uncommitted_work_warned "$A_OFF") ))
O_LOG="$(wgit "$A_OFF" log --format=%s | tr '\n' '|')"
O_STATUS="$(wgit "$A_OFF" status --porcelain | tr '\n' ' ')"
rep_block "Worktree of the off case after it completed (the declared file is still untracked: no commit attempt):" "$(wgit "$A_OFF" status --porcelain; echo ---; wgit "$A_OFF" log --format='%h %s')"
[[ $OD == 0 ]]; ck 6 $? "off: the task and the Automaton complete" "Automaton \`$(prd_status "$A_OFF")\`, task \`$(task_field "$A_OFF" '.status')\`"
[[ "$O_DEC" != *uncommitted_work* && "$O_AUD" == 0 ]]; ck 6 $? "off: no decision and no audit event of the check" "decisions \`$O_DEC\`; events $O_AUD"
[[ "$O_STATUS" == *"notes/"* ]]; ck 6 $? "off: no commit attempt (the declared file that the executor commits under hold stays untracked)" "git status \`$O_STATUS\`; commits \`$O_LOG\`"
[[ "$(uw_status | jq_r '.held | length')" == 0 && "$(uw_status | jq_r '.policy')" == off ]]; ck 6 $? "off: nothing is held" "policy \`$(uw_status | jq_r '.policy')\`"
api PUT /api/config "{\"$POL\":\"hold\"}" >/dev/null

# ============================================================================
CUR=0
FINAL_REFS="$(gitr for-each-ref --format='%(refname:short)' refs/heads/ | tr '\n' ' ')"
DAEMON_LOG_TAIL="$(dlog | grep -E 'uncommitted-work' | cut -c1-200 | tail -12)"

begin_check 7 "Cleanup"
td_stop_all
sleep 1
D_GONE="no"; td_alive "$DPID" || D_GONE="yes"
PORT_CLOSED="no"; td_wait_port_closed "$TLS_PORT" 20 && PORT_CLOSED="yes"
LEFT_OUT="$(env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" check "$LEFT_SNAP" 2>&1)"; LEFT_RC=$?
[[ "$D_GONE" == yes && "$PORT_CLOSED" == yes && "$LEFT_RC" == 0 ]]; ck 7 $? "the sandbox daemon and its sessions are gone and no test daemon was left behind" "daemon gone: $D_GONE; port $TLS_PORT closed: $PORT_CLOSED; leftover check exit $LEFT_RC"
rep ""
rep "- The daemon was ended by \`td_stop_all\` from \`scripts/test-daemon-lib.sh\` (the recorded pid, config path as marker); nothing was ended by name."
rep "- The scratch area (project, remote, worktrees, data, config, log, records, worker, state) is removed by the exit handler right after this report is written."

# --- assemble the report ---------------------------------------------------
OVERALL=PASS; TOTAL_FAIL=0
for n in 1 2 3 4 5 6 7; do TOTAL_FAIL=$(( TOTAL_FAIL + ${CK_FAIL[$n]:-0} )); done
[[ $TOTAL_FAIL -gt 0 ]] && OVERALL=FAIL
# every numbered check must have run at least one assertion
for n in 1 2 3 4 5 6 7; do [[ "${CK_TOTAL[$n]:-0}" -gt 0 ]] || { OVERALL=FAIL; CK_FAIL[$n]=1; }; done

{
  printf '# Automaton uncommitted-work check — live check on a sandbox daemon\n\n'
  printf '**Date:** %s (run started %s, local time)\n' "$(date '+%Y-%m-%d')" "$DAEMON_UP_AT"
  printf '**Version under test:** %s (binary built from this worktree)\n' "${VERSION:-unknown}"
  printf '**Script:** `scripts/uncommitted-work-live-check.sh` (run as `env -u TMUX -u TMUX_TMPDIR bash scripts/uncommitted-work-live-check.sh`)\n'
  printf '**Scope:** the uncommitted-work policy and scope settings, `GET /api/autonomous/uncommitted-work` and its MCP, CLI and chat reads, a held task, the commit, discard and retry answers, the whole-worktree block before an Automaton is reported finished, and the warn and off modes, against a throwaway project repository whose default branch is `%s`.\n\n' "$PROJECT_DEFAULT_BRANCH"
  printf '## Result\n\n**Overall:** %s\n\n' "$OVERALL"
  printf '| # | Check | Result | Assertions |\n|---|---|---|---|\n'
  for n in 1 2 3 4 5 6 7; do
    v=PASS; [[ "${CK_FAIL[$n]:-0}" -gt 0 ]] && v=FAIL
    printf '| %s | %s | %s | %s of %s passed |\n' "$n" "${CK_TITLE[$n]}" "$v" "$(( ${CK_TOTAL[$n]:-0} - ${CK_FAIL[$n]:-0} ))" "${CK_TOTAL[$n]:-0}"
  done
  printf '\n'
  printf '## Setup\n\n'
  printf -- '- **Sandbox daemon only.** Started with `td_start` from `scripts/test-daemon-lib.sh` from a config generated from `testdata/datawatch.yaml` by the substitutions the test runner uses (data folder, ports, loopback host, test token). Added: `autonomous.worktree_base_repo` and `autonomous.worktree_dir` (the throwaway project and its worktree root), `autonomous.questions.enabled` (the operator question routes are off by default), `autonomous.verification_backend: shell`, `server.allowed_project_roots`, and `shell_backend.script_path` (the stand-in worker). The production daemon, its service and its data were not touched, and nothing was ended by name.\n'
  printf -- '- **Ports.** HTTP %s, TLS %s, MCP %s, DNS %s, webhook %s, notify %s, each probed free just before use.\n' "$PORT" "$TLS_PORT" "$MCP_PORT" "$DNS_PORT" "$HOOK_PORT" "$NTFY_PORT"
  printf -- '- **Session sockets.** The daemon ran with its multiplexer socket directory under the throwaway folder, so no session of the operator was reachable and none was touched. The script itself issues no multiplexer command.\n'
  printf -- '- **Everything** lives under one temporary folder outside the worktree, removed on every exit path.\n'
  printf -- '- **Throwaway project.** A bare repository whose default branch is `%s`, cloned to the worktree base repository; `refs/remotes/origin/HEAD` records it and `main` does not exist.\n' "$PROJECT_DEFAULT_BRANCH"
  printf -- '- **Worker.** The daemon'"'"'s shell backend runs a generated script as the worker. Each Automaton declares the write directory `%s/`; the script writes its sample file there (the session'"'"'s own commit stages it) and, in the stray modes, a stray file at the worktree root, outside that directory (never staged). Every task carries a deterministic verification script, and the daemon'"'"'s verification (a one-shot session on the same backend, `autonomous.verification_backend: shell`) is answered by the same script with a fixed pass, so no model is involved anywhere.\n' "$WRITE_SUBDIR"
  printf '\n'
  printf '```\n%s\n```\n\n' "$DEFAULT_PROBE"
  printf '| Automaton | Worker mode | Used in |\n|---|---|---|\n'
  for row in "${AUTOMATA[@]}"; do
    IFS='|' read -r aid aname amode afiles <<< "$row"
    case "$aid" in
      "$A_COMMIT") use="checks 3, 4 (commit)";; "$A_DISCARD") use="checks 3, 4 (discard), 5";; "$A_RETRY") use="checks 3, 4 (retry)";;
      "$A_WARN") use="check 6 (warn)";; "$A_OFF") use="check 6 (off)";; *) use="check 6 (control, policy hold)";;
    esac
    printf '| `%s` %s | %s | %s |\n' "$aid" "$aname" "$amode" "$use"
  done
  printf '\n'
  cat "$S/rep-0.md" 2>/dev/null
  for n in 1 2 3 4 5 6 7; do cat "$S/rep-$n.md" 2>/dev/null; done
  printf '\n## Daemon log, the uncommitted-work lines\n\n```\n%s\n```\n' "$DAEMON_LOG_TAIL"
  printf '\n## Spec amendments and what is not shown live\n\n'
  printf -- '- **How the held state was made.** The shell backend ran a generated worker script headlessly in the worktree the daemon created, so no state had to be staged by hand. The stray file is outside the Automaton'"'"'s declared write directory, which is what keeps the session'"'"'s own commit from staging it and what makes the executor treat it as outside the task'"'"'s declared files (it never commits those by itself).\n'
  printf -- '- **The Automata were written into the sandbox store, not created through the create route.** The route only makes drafts and a plan needs a planner model, which a sandbox does not have. Approve, run, the answers, every read and both settings go through the REST API of the running daemon; the worktrees and branches were made by the daemon.\n'
  printf -- '- **Check 5 reaches the whole-worktree block with the sibling file of the discard case.** A file that appears in the worktree after the hold was placed is left alone by discard (check 4), so it is still there when the task completes and the Automaton rolls up: the same state a cancel or a backend move leaves behind. The merge list is read with the merge-pending mode set to `shadow`, which lists completed Automata with a branch.\n'
  printf -- '- **Warn and off are shown on one Automaton each**, with the policy changed through the config route between runs. Off is shown as "no commit attempt" by a file the task declared: under hold (the control run) the executor commits it, under off it stays untracked.\n'
  printf -- '- **Two defects the live runs found, fixed in the daemon (each has a unit test).** First, the check counted the files the daemon itself writes into a worktree when a session starts (the MCP discovery file, injected skills, and the lines it adds to the ignore file) as uncommitted work, so the first task of any project that does not already ignore them was held; the detector now leaves them out, and check 3 asserts that only the stray file is listed. Second, when the whole-worktree hold raised its question, the question store'"'"'s own save of the Automaton made the roll-up read another writer and abort, which left the Automaton `running` with nothing to run instead of `blocked`; the hold now counts that save as its own, and check 5 asserts the blocked state.\n'
  printf -- '- **Not shown live:** a failing commit hook (the commit-failed hold and its fresh question after a failed answer), a discard that is refused because a listed path leaves the task directory, an expired or withdrawn question keeping its hold, the `worktree` scope on a task, a second hold on an Automaton that already carries one, and the PWA card. These are covered by the unit tests of the check and by the PWA tests; this script drives the daemon through REST, MCP, the command line and chat only.\n'
  printf -- '- **Generality.** The project is a throwaway repository whose default branch is `%s` and has no `main`; the base repository and the worktree root come from the daemon configuration; nothing names the repository this script lives in.\n' "$PROJECT_DEFAULT_BRANCH"
  printf '\nResult: %s\n' "$OVERALL"
} > "$S/report.md"

# No absolute path and no temporary folder name in the tracked report.
SCRUB="$S/scrub.sed"
{
  printf 's|%s|<sandbox>|g\n' "$S"
  printf 's|%s|<repo>|g\n' "$ROOT"
  [[ -n "${HOME:-}" ]] && printf 's|%s|<home>|g\n' "$HOME"
  printf 's|/tmp/[^ `"'"'"')]*|<temp>|g\n'
  printf 's|/home/[^ `"'"'"')]*|<path>|g\n'
} > "$SCRUB"
mkdir -p "$(dirname "$REPORT")"
sed -f "$SCRUB" "$S/report.md" > "$REPORT"

say ""
say "Overall: $OVERALL — report: docs/plans/$(basename "$REPORT")"
[[ "$OVERALL" == PASS ]]
