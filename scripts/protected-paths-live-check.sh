#!/usr/bin/env bash
# protected-paths-live-check.sh — live check of the Automaton protected-paths
# hold on a SANDBOX daemon, against throwaway project repositories.
#
# What it proves (each result is a numbered check in the report):
#   1. Config: protected_paths, protected_paths_action and rules_review_source
#      round-trip through PUT and GET /api/config, an unknown word is a 400 that
#      names the allowed words, the defaults read hold, base_commit and the
#      default list on a fresh config.
#   2. Read surfaces agree: GET /api/autonomous/protected-paths (200 shape, 405
#      for POST), the MCP tool, the CLI with --json and the chat verb through
#      POST /api/test/message return the same held list.
#   3. Hold: an Automaton in worktree mode whose worker edits a protected file is
#      held (task blocked, hold object, ONE question with the options approve,
#      reject and change_spec that lists the path); the same in direct mode.
#   4. Answers: approve completes the task; reject puts the file back to its
#      base content in a new commit and the task runs again; change_spec leaves
#      the task blocked.
#   5. Authorised path: after set_authorized_paths the same edit passes with no
#      question; clearing the list brings the hold back.
#   6. Modes: warn completes with a recorded decision and no question; block
#      blocks with no question; off does nothing.
#   7. Different names: protected_paths set to the throwaway project's own rules
#      file name and a directory; an edit to a file named AGENT.md, outside the
#      list, passes.
#   8. Judge: recorded honestly as covered by the Go tests with fakes (the
#      sandbox has no judge model); no live judge result is claimed.
#   9. Cleanup: the sandbox daemon is gone and no test daemon was left behind.
#
# How the held state is made. The worker is the daemon's own shell backend
# pointed at a small generated script (shell_backend.script_path), so a worker
# runs headlessly and deterministically. The throwaway project tracks a rules
# file (RULES.md, named by this script and set as autonomous.rules_file), an
# AGENT.md-named file and a policy/ directory; the worker edits one of them
# depending on the mode named in the task text.
#
# The Automata are written into the sandbox's own store before its first start
# (the create route only makes drafts and a plan needs a planner model, which a
# sandbox does not have). From the moment the daemon is up everything goes
# through its REST API (approve, run, answers, reads, settings). Worktrees are
# made by the daemon itself (autonomous.worktree_base_repo); the direct-mode
# Automaton points at its own clone of the throwaway project.
#
# Generality: nothing here names this repository. The project is a throwaway
# repository whose default branch is NOT "main" (found by git from
# refs/remotes/origin/HEAD).
#
# Safety: SANDBOX only. The daemon runs from a generated config under a
# throwaway directory on loopback ports, with its own data directory, its own
# TMPDIR and its own multiplexer socket directory. The production daemon, its
# service and its data are never touched. The daemon is ended only by the PID
# that scripts/test-daemon-lib.sh recorded. Everything this script made is
# removed on every exit path.
#
# Usage (always from a shell with the operator's multiplexer variables removed):
#   env -u TMUX -u TMUX_TMPDIR bash scripts/protected-paths-live-check.sh
#
# Env:
#   PROTECTED_PATHS_CHECK_REPORT  where the tracked report is written
#                                 (default: docs/plans/2026-10-31-automaton-protected-paths-live-check.md)
#   TEST_BINARY                   daemon binary (default: bin/datawatch, built
#                                 from this worktree when missing, then PATH)
#
# Exit 0 only when every check passed.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/scripts/test-daemon-lib.sh"
REPORT="${PROTECTED_PATHS_CHECK_REPORT:-$ROOT/docs/plans/2026-10-31-automaton-protected-paths-live-check.md}"

# The throwaway project's default branch: deliberately not "main".
PROJECT_DEFAULT_BRANCH="trunk"
# The directory every Automaton declares as its write scope.
WRITE_SUBDIR="src"
# The deterministic verification script every task carries (tracked in the project).
VERIFY_SCRIPT=".checks/ok.sh"

unset TMUX TMUX_TMPDIR

# --- scratch area and lifecycle ----------------------------------------------
# When started in the background SIGINT is ignored, and the daemon library then
# restarts the script from the top to restore it. Do that restart here, before
# anything is created, so the first start leaves no scratch folder behind.
if [[ "${TD_REEXEC_GUARD:-}" != "$$" ]]; then
  _sigign="$(sed -n 's/^SigIgn:[[:space:]]*//p' "/proc/$$/status" 2>/dev/null | sed -n 1p)"
  if [[ "$_sigign" =~ ^[0-9a-fA-F]+$ ]] && (( (16#$_sigign & 2) != 0 )) && command -v python3 >/dev/null 2>&1; then
    export TD_REEXEC_GUARD=$$
    exec python3 -I -c 'import os, signal, sys
signal.signal(signal.SIGINT, signal.SIG_DFL)
os.execvp("bash", ["bash"] + sys.argv[1:])' "$0" "$@"
  fi
fi
S="$(mktemp -d "${TMPDIR:-/tmp}/dw-protected-live.XXXXXX")"
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

# The throwaway project's default branch: deliberately not "main".
PROJECT_DEFAULT_BRANCH="trunk"
# The project's own rules file (named here, set as autonomous.rules_file), an
# AGENT.md-named file that is NOT the rules file, and a directory.
RULES_FILE="RULES.md"
OTHER_FILE="AGENT.md"
POLICY_DIR="policy"
WRITE_SUBDIR="src"
VERIFY_SCRIPT=".checks/ok.sh"

# --- the Automata ----------------------------------------------------------
# id|name|worker mode|kind (worktree|direct)
#   modes: rules-once  edits the rules file on its first run only
#          rules       edits the rules file on every run
#          other       edits the AGENT.md-named file
#          policy      edits a file under policy/
AUTOMATA=(
  "e0c10001|hold-approve|rules-once|worktree"
  "e0c10002|hold-reject|rules-once|worktree"
  "e0c10003|hold-spec|rules|worktree"
  "e0c10004|authorised|rules|worktree"
  "e0c10005|authorised-cleared|rules|worktree"
  "e0c10006|mode-warn|rules|worktree"
  "e0c10007|mode-block|rules|worktree"
  "e0c10008|mode-off|rules|worktree"
  "e0c10009|names-other|other|worktree"
  "e0c1000a|names-policy|policy|worktree"
  "e0c1000b|hold-direct|rules-once|direct"
)
A_APPROVE=e0c10001; A_REJECT=e0c10002; A_SPEC=e0c10003; A_AUTH=e0c10004; A_CLEARED=e0c10005
A_WARN=e0c10006; A_BLOCK=e0c10007; A_OFF=e0c10008; A_OTHER=e0c10009; A_POLICY=e0c1000a; A_DIRECT=e0c1000b
tid() { printf '%st1' "$1"; }

DIRECT="$PROJ/direct-project"   # the direct-mode Automaton's own clone

build_project() {
  git init -q --bare -b "$PROJECT_DEFAULT_BRANCH" "$REMOTE"
  git clone -q "$REMOTE" "$REPO" 2>/dev/null
  gitr config user.email "protected-live-check@example.invalid"
  gitr config user.name "protected live check"
  gitr config commit.gpgsign false
  gitr checkout -q -b "$PROJECT_DEFAULT_BRANCH"
  mkdir -p "$REPO/$WRITE_SUBDIR" "$REPO/$POLICY_DIR" "$REPO/$(dirname "$VERIFY_SCRIPT")"
  printf 'readme\n' > "$REPO/README.md"
  printf 'base rules\n' > "$REPO/$RULES_FILE"
  printf 'base agent notes\n' > "$REPO/$OTHER_FILE"
  printf 'base policy\n' > "$REPO/$POLICY_DIR/policy.md"
  printf 'sample directory\n' > "$REPO/$WRITE_SUBDIR/README.md"
  # A project keeps the daemon's per-session files out of its commits, the
  # way a real one does; the worker's write directory is the whole worktree.
  printf '.mcp.json\n.datawatch/\n' > "$REPO/.gitignore"
  printf '#!/usr/bin/env bash\nexit 0\n' > "$REPO/$VERIFY_SCRIPT"
  gitr add -A
  gitr commit -q -m "Initial commit"
  gitr push -q -u origin "$PROJECT_DEFAULT_BRANCH"
  gitr remote set-head origin "$PROJECT_DEFAULT_BRANCH" >/dev/null
  git clone -q "$REMOTE" "$DIRECT" 2>/dev/null
  git -C "$DIRECT" config user.email "protected-live-check@example.invalid"
  git -C "$DIRECT" config user.name "protected live check"
  git -C "$DIRECT" config commit.gpgsign false
}

write_worker() {
  cat > "$S/bin/worker.sh" <<WORKER
#!/bin/sh
task="\$1"; dir="\$2"
tok="\$(printf '%s' "\$task" | grep -o 'LIVECHECK\[[a-z-]*:[a-z0-9]*\]' | head -1)"
mode="\$(printf '%s' "\$tok" | sed 's/^LIVECHECK\[\(.*\):.*\]\$/\1/')"
id="\$(printf '%s' "\$tok" | sed 's/^LIVECHECK\[.*:\(.*\)\]\$/\1/')"
case "\$task" in
  *.verify-output-*)
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
  rules-once) [ "\$first" = yes ] && printf 'edit by %s\n' "\$id" >> "$RULES_FILE" ;;
  rules) printf 'edit by %s\n' "\$id" >> "$RULES_FILE" ;;
  other) printf 'edit by %s\n' "\$id" >> "$OTHER_FILE" ;;
  policy) printf 'edit by %s\n' "\$id" >> "$POLICY_DIR/policy.md" ;;
esac
exit 0
WORKER
  chmod +x "$S/bin/worker.sh"
}

seed_store() {
  mkdir -p "$DATA/autonomous"
  python3 -I - "$DATA/autonomous/prds.jsonl" "$WTROOT" "$DIRECT" "$VERIFY_SCRIPT" "${AUTOMATA[@]}" <<'PY'
import json, sys, datetime
out, wtroot, direct, verify, rows = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5:]
now = datetime.datetime.now(datetime.timezone.utc).isoformat()
with open(out, "w") as f:
    for row in rows:
        aid, name, mode, kind = row.split("|")
        task = {
            "id": aid + "t1", "story_id": aid + "s1", "prd_id": aid,
            "title": "Write the sample file for " + name,
            "spec": "Write the sample file. LIVECHECK[%s:%s]" % (mode, aid),
            "status": "pending", "verification_script": verify,
            "created_at": now, "updated_at": now,
        }
        rec = {
            "id": aid, "title": name, "spec": "Live check sample " + name + " LIVECHECK[" + mode + ":" + aid + "]",
            "project_dir": direct if kind == "direct" else "", "status": "needs_review", "backend": "shell",
            "write_dirs": [direct if kind == "direct" else wtroot + "/" + aid],
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
  sed -i \
    -e "s|^autonomous:\$|autonomous:\n  verification_backend: shell\n  rules_file: $RULES_FILE\n  questions:\n    enabled: true\n  worktree_base_repo: $REPO\n  worktree_dir: $WTROOT|" \
    -e "s|^server:\$|server:\n  allowed_project_roots: [\"$PROJ\", \"$WTROOT\"]|" \
    -e "s|^shell_backend:\$|shell_backend:\n  script_path: $S/bin/worker.sh|" \
    "$CFG"
}

# --- reads ---------------------------------------------------------------
cfg_get() { api GET /api/config | jq_r "$1"; }
pp_status() { api GET /api/autonomous/protected-paths; }
prd() { api GET "/api/autonomous/prds/$1"; }
prd_status() { prd "$1" | jq_r '.status'; }
task_json() { prd "$1" | jq -c --arg t "$(tid "$1")" '[.stories[]?.tasks[]? | select(.id==$t)][0] // {}'; }
task_field() { task_json "$1" | jq_r "$2"; }
hold_question() { task_field "$1" '.protected_path_hold.question_id // empty'; }
aud_count() { api GET "/api/audit?action=$1&limit=100" | jq_r --arg a "$1" '[.entries[]? | select(.details.prd_id=="'"$2"'")] | length'; }
decisions() { prd "$1" | jq_r '[.decisions[]?.kind] | join(" ")'; }
dlog() { cat "$LOG" "$DATA"/*.log 2>/dev/null; }
# questions of one task (any state), by the task id the question carries
task_questions() { api GET "/api/questions?limit=500" | jq -c --arg t "$(tid "$1")" '[(.questions // .)[]? | select((.task_id // .TaskID // "")==$t)]' 2>/dev/null || echo '[]'; }
task_question_count() { task_questions "$1" | jq_r 'length'; }

is_held() { [[ "$(task_field "$1" '.status')" == blocked && "$(task_field "$1" '.protected_path_hold.paths | length')" -ge 1 && -n "$(hold_question "$1")" ]]; }
is_done() { [[ "$(prd_status "$1")" == completed ]]; }
task_done() { [[ "$(task_field "$1" '.status')" == completed ]]; }
is_blocked_noq() { [[ "$(task_field "$1" '.status')" == blocked ]]; }
worker_runs() { grep -c "^$1 " "$STATE/runs.log" 2>/dev/null || true; }
chat() { api POST /api/test/message "{\"text\":\"$1\"}" | jq -r '.responses | join("\n")' 2>/dev/null; }

diag() {
  say "  DIAG $1: Automaton $(prd_status "$1"); task $(task_json "$1" | jq -c '{status, error, wait_reason, failure_reason, protected_path_hold}')"
  dlog | grep -F "$1" | tail -8 | cut -c1-240 | sed 's/^/    log: /'
}

start_automaton() {
  local code_a code_r
  code_a="$(api_code POST "/api/autonomous/prds/$1/approve" '{}')"
  code_r="$(api_code POST "/api/autonomous/prds/$1/run" '{}')"
  say "  approve $code_a, run $code_r ($1)"
  [[ "$code_a" == 200 && "$code_r" == 200 ]]
}
answer() { api_code POST "/api/questions/$1/answer" "{\"chosen_option_id\":\"$2\",\"actor\":\"operator\",\"surface\":\"cli\"}"; }
set_action() { api_code PUT /api/config "{\"autonomous.automation.protected_paths_action\":\"$1\"}"; }
set_auth() { # set_auth <id> <json array>
  api_both POST "/api/autonomous/prds/$1/set_authorized_paths" "{\"paths\":$2}"
}
# wait for a run that must NOT hold: finished, whatever the outcome
settled() { local st; st="$(prd_status "$1")"; [[ "$st" == completed || "$st" == blocked || "$st" == failed ]]; }

# ============================================================================
say "protected-paths live check — $(now) — binary: $BINARY ($VERSION)"
say "ports: http $PORT tls $TLS_PORT mcp $MCP_PORT"

LEFT_SNAP="$S/leftover.snap"
env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" snapshot "$LEFT_SNAP" >/dev/null 2>&1 || true

CUR=0
build_project
write_worker
seed_store
write_config
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

PA="autonomous.automation.protected_paths"; PAC="autonomous.automation.protected_paths_action"; RRS="autonomous.automation.rules_review_source"

# ============================================================================
begin_check 1 "Config: protected_paths, protected_paths_action, rules_review_source"
D_ST="$(pp_status)"
D_ACT="$(printf '%s' "$D_ST" | jq_r '.action')"; D_SRC="$(printf '%s' "$D_ST" | jq_r '.rules_source')"
D_LIST="$(printf '%s' "$D_ST" | jq -c '.paths')"
printf '%s' "$D_ST" | jq -e --arg r "$RULES_FILE" '.paths | (index($r) != null) and (index("CODEOWNERS") != null) and (index(".github/workflows/") != null) and (map(select(contains("supervisor-rules"))) | length == 1)' >/dev/null 2>&1; DL=$?
[[ "$D_ACT" == hold && "$D_SRC" == base_commit ]]; ck 1 $? "a fresh config reads action hold and rules source base_commit" "status route: action=\`$D_ACT\` rules_source=\`$D_SRC\`; raw settings before any write: action=\`$(cfg_get '.autonomous.automation.protected_paths_action // "unset"')\` source=\`$(cfg_get '.autonomous.automation.rules_review_source // "unset"')\` (unset reads as the default)"
ck 1 $DL "a fresh config reads the default list: the project's rules file, the supervisor rules document, CODEOWNERS and the workflows directory" "default list: $D_LIST"
RT_OK=0; RT_TXT=""
for w in warn block off hold; do
  code="$(api_code PUT /api/config "{\"$PAC\":\"$w\"}")"; got="$(cfg_get '.autonomous.automation.protected_paths_action')"; eff="$(pp_status | jq_r '.action')"
  RT_TXT+="$w: HTTP $code, read \`$got\`, status \`$eff\`; "
  [[ "$code" == 200 && "$got" == "$w" && "$eff" == "$w" ]] || RT_OK=1
done
ck 1 $RT_OK "PUT then GET round-trips protected_paths_action (warn, block, off, hold)" "$RT_TXT"
RT_OK=0; RT_TXT=""
for w in worktree base_commit; do
  code="$(api_code PUT /api/config "{\"$RRS\":\"$w\"}")"; got="$(cfg_get '.autonomous.automation.rules_review_source')"; eff="$(pp_status | jq_r '.rules_source')"
  RT_TXT+="$w: HTTP $code, read \`$got\`, status \`$eff\`; "
  [[ "$code" == 200 && "$got" == "$w" && "$eff" == "$w" ]] || RT_OK=1
done
ck 1 $RT_OK "PUT then GET round-trips rules_review_source (worktree, base_commit)" "$RT_TXT"
code="$(api_code PUT /api/config "{\"$PA\":[\"docs/\",\"notes.md\"]}")"; got="$(cfg_get '.autonomous.automation.protected_paths | tostring')"; eff="$(pp_status | jq -c '.paths')"
[[ "$code" == 200 && "$got" == '["docs/","notes.md"]' && "$eff" == '["docs/","notes.md"]' ]]; ck 1 $? "PUT then GET round-trips protected_paths (a list) and the status route shows it as the effective list" "HTTP $code, read $got, effective list $eff"
code="$(api_code PUT /api/config "{\"$PA\":[]}")"; eff="$(pp_status | jq -c '.paths')"
[[ "$code" == 200 && "$eff" == "$D_LIST" ]]; ck 1 $? "an empty list clears protected_paths back to the default list" "HTTP $code, effective list $eff"
B_ACT="$(api_both PUT /api/config "{\"$PAC\":\"sometimes\"}")"; B_SRC="$(api_both PUT /api/config "{\"$RRS\":\"elsewhere\"}")"
AFTER_ACT="$(pp_status | jq_r '.action')"; AFTER_SRC="$(pp_status | jq_r '.rules_source')"
[[ "$B_ACT" == 400* && "$B_ACT" == *hold* && "$B_ACT" == *warn* && "$B_ACT" == *block* && "$B_ACT" == *off* ]]; ck 1 $? "an unknown action word is a 400 that names hold, warn, block and off" "\`PUT /api/config\`: $B_ACT"
[[ "$B_SRC" == 400* && "$B_SRC" == *base_commit* && "$B_SRC" == *worktree* ]]; ck 1 $? "an unknown rules source word is a 400 that names base_commit and worktree" "\`PUT /api/config\`: $B_SRC"
[[ "$AFTER_ACT" == hold && "$AFTER_SRC" == base_commit ]]; ck 1 $? "the refused writes saved nothing" "action \`$AFTER_ACT\`, rules source \`$AFTER_SRC\` after the refused writes"

# ============================================================================
# Hold phase: five Automata run until each task is held (or, for the one with an
# authorised list, until it has finished).
CUR=0
set_auth "$A_AUTH" "[\"$RULES_FILE\"]" >/dev/null
set_auth "$A_CLEARED" "[\"$RULES_FILE\"]" >/dev/null
CLR="$(set_auth "$A_CLEARED" '[]')"
for id in "$A_APPROVE" "$A_REJECT" "$A_SPEC" "$A_DIRECT" "$A_CLEARED"; do start_automaton "$id" || say "  WARN: $id did not start"; done
declare -A HELD_AT=()
HELD_DEADLINE=$((SECONDS + 420))
while ((SECONDS < HELD_DEADLINE)); do
  ALL=1
  for id in "$A_APPROVE" "$A_REJECT" "$A_SPEC" "$A_DIRECT" "$A_CLEARED"; do
    if [[ -z "${HELD_AT[$id]:-}" ]]; then
      if is_held "$id"; then HELD_AT[$id]="$(now)"; say "  $id held at ${HELD_AT[$id]}"; else ALL=0; fi
    fi
  done
  [[ $ALL == 1 ]] && break
  sleep 2
done
for id in "$A_APPROVE" "$A_REJECT" "$A_SPEC" "$A_DIRECT" "$A_CLEARED"; do [[ -n "${HELD_AT[$id]:-}" ]] || diag "$id"; done

hold_checks() { # hold_checks <id> <label>
  local id="$1" label="$2" T Q QJ n
  T="$(task_json "$id")"; Q="$(hold_question "$id")"
  rep_block "Task JSON of the held task, $label (\`protected_path_hold\` is the hold):" "$(printf '%s' "$T" | jq '{id, status, error, protected_path_hold}')"
  [[ "$(printf '%s' "$T" | jq_r '.status')" == blocked && "$(printf '%s' "$T" | jq_r '.completed_at // "none"')" == none ]]
  ck 3 $? "$label: the task is held (blocked), not completed" "status \`$(printf '%s' "$T" | jq_r '.status')\`, error \`$(printf '%s' "$T" | jq_r '.error')\`"
  printf '%s' "$T" | jq -e --arg f "$RULES_FILE" '.protected_path_hold.paths == [$f] and .protected_path_hold.action == "hold" and (.protected_path_hold.base_commit|length > 0)' >/dev/null 2>&1
  ck 3 $? "$label: the hold object names the changed protected path, the action and the base commit" "hold: $(printf '%s' "$T" | jq -c '.protected_path_hold | {paths, action, base_commit, reason}')"
  QJ="$(api GET "/api/questions/$Q")"
  rep_block "The question, $label (\`GET /api/questions/<id>\`):" "$(printf '%s' "$QJ" | jq '.question | {id, label, state, default, context, options: [.options[]? | .id], text}' 2>/dev/null || printf '%s' "$QJ")"
  printf '%s' "$QJ" | jq -e '[.question.options[].id] == ["approve","reject","change_spec"]' >/dev/null 2>&1; local o1=$?
  [[ "$(printf '%s' "$QJ" | jq_r '.question.default')" == hold && "$(printf '%s' "$QJ" | jq_r '.question.state')" == open && "$(printf '%s' "$QJ" | jq_r '.question.label')" == protected_paths ]]; local o2=$?
  printf '%s' "$QJ" | jq -e --arg f "$RULES_FILE" '.question.text | contains($f)' >/dev/null 2>&1; local o3=$?
  ck 3 $((o1 + o2 + o3)) "$label: an open question offers approve, reject and change_spec (default hold) and lists the path" "question \`$Q\`: options $(printf '%s' "$QJ" | jq -c '[.question.options[]?.id]'), default \`$(printf '%s' "$QJ" | jq_r '.question.default')\`, state \`$(printf '%s' "$QJ" | jq_r '.question.state')\`, text names \`$RULES_FILE\`: $([[ $o3 == 0 ]] && echo yes || echo no)"
  n="$(task_question_count "$id")"
  [[ "$n" == 1 ]]; ck 3 $? "$label: exactly ONE question exists for the task" "questions carrying the task id: $n"
  [[ "$(prd_status "$id")" != completed ]]; ck 3 $? "$label: the Automaton is not completed" "status \`$(prd_status "$id")\`"
}

begin_check 3 "Hold: an edit to a protected file is held, in worktree mode and in direct mode"
rep ""
rep "Approach used: the **shell backend with a generated worker script** ran each task headlessly. The worker appends a line to \`$RULES_FILE\`, the project's own rules file (set as \`autonomous.rules_file\`, so it is on the default protected list). Worktree mode: the daemon made the worktree from the throwaway project. Direct mode: the Automaton's project directory is its own clone of the throwaway project."
hold_checks "$A_APPROVE" "worktree mode"
hold_checks "$A_DIRECT" "direct mode"
for id in "$A_REJECT" "$A_SPEC"; do is_held "$id"; ck 3 $? "the same hold is placed on the other worktree Automata (\`$id\`)" "task status \`$(task_field "$id" '.status')\`, paths $(task_field "$id" '.protected_path_hold.paths | tostring')"; done
AUDH="$(aud_count autonomous_protected_paths_held "$A_APPROVE")"; DEC="$(decisions "$A_APPROVE")"
[[ "$AUDH" -ge 1 && "$DEC" == *protected_paths_held* ]]; ck 3 $? "the hold is in the audit log and the decision log" "audit events $AUDH; decisions \`$DEC\`"
is_held "$A_CLEARED"; CLEARED_RC=$?; CLEARED_EV="clear answered \`$CLR\`; task status \`$(task_field "$A_CLEARED" '.status')\`, hold paths $(task_field "$A_CLEARED" '.protected_path_hold.paths | tostring')"

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
if "action: %s" % r["action"] not in chat_text: problems.append("chat reply misses the action")
for e in r.get("held", []):
    if e["automaton_id"] not in chat_text: problems.append("chat reply misses " + e["automaton_id"])
    for f in e["paths"]:
        if f not in chat_text: problems.append("chat reply misses path " + f)
    if e.get("question_id") and e["question_id"] not in chat_text: problems.append("chat reply misses question " + e["question_id"])
if ("Held: %d" % len(r.get("held", []))) not in chat_text: problems.append("chat reply misses the held count")
print("OK" if not problems else "; ".join(problems))
sys.exit(0 if not problems else 1)
PY
mcp_read() {
  env -u TMUX -u TMUX_TMPDIR TMUX_TMPDIR="$SOCKDIR" python3 -I - "$BINARY" "$CFG" <<'PY'
import json, subprocess, sys, os, select, time
bin_, cfg = sys.argv[1], sys.argv[2]
p = subprocess.Popen([bin_, "--config", cfg, "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                     stderr=subprocess.DEVNULL, env=dict(os.environ), text=True)
def send(o): p.stdin.write(json.dumps(o) + "\n"); p.stdin.flush()
send({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2024-11-05", "capabilities": {}, "clientInfo": {"name": "protected-paths-live-check", "version": "0"}}})
send({"jsonrpc": "2.0", "method": "notifications/initialized"})
send({"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": "autonomous_protected_paths", "arguments": {}}})
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
REST_CODE="$(api_code GET /api/autonomous/protected-paths)"; POST_CODE="$(api_code POST /api/autonomous/protected-paths '{}')"
SHAPE_OK=1; pp_status | jq -e '(.action|type=="string") and (.rules_source|type=="string") and (.paths|type=="array") and (.held|type=="array") and (.authorized|type=="array") and all(.held[]; has("automaton_id") and has("task_id") and (.paths|type=="array") and has("action") and has("question_id") and has("since") and has("awaiting_spec_change"))' >/dev/null 2>&1 && SHAPE_OK=0
[[ "$REST_CODE" == 200 ]]; ck 2 $? "\`GET /api/autonomous/protected-paths\` answers 200" "HTTP $REST_CODE"
ck 2 $SHAPE_OK "the body has the action, the rules source, the list, a held list whose entries carry the Automaton, task, paths, action, question and time, and an authorised list" "$(pp_status | jq -c '{action, rules_source, held: [.held[] | {automaton_id, task_id, paths}], authorized}')"
[[ "$POST_CODE" == 405 ]]; ck 2 $? "POST is a 405" "HTTP $POST_CODE"
SV=""
for try in 1 2 3; do
  api GET /api/autonomous/protected-paths > "$S/s-rest.json"
  dw autonomous protected-paths --json > "$S/s-cli.json" 2> "$S/s-cli.err"
  mcp_read > "$S/s-mcp.json"
  chat "autonomous protected-paths" > "$S/s-chat.txt"
  SV="$(python3 -I "$SURF_PY" "$S/s-rest.json" "$S/s-cli.json" "$S/s-mcp.json" "$S/s-chat.txt" 2>&1)"
  [[ "$SV" == OK ]] && break
  sleep 2
done
rep_block "CLI text (\`datawatch autonomous protected-paths\`):" "$(dw autonomous protected-paths 2>&1)"
rep_block "Chat reply (\`POST /api/test/message\`, text \`autonomous protected-paths\`):" "$(cat "$S/s-chat.txt")"
rep_block "MCP result (\`autonomous_protected_paths\`, first 700 bytes):" "$(head -c 700 "$S/s-mcp.json")"
[[ "$SV" == OK ]]; ck 2 $? "REST, MCP, CLI (--json) and the chat verb return the same held list" "comparison: \`$SV\`; held entries: $(jq -c '[.held[] | {automaton_id, task_id}]' "$S/s-rest.json" 2>/dev/null)"
N_FILTER="$(api GET "/api/autonomous/protected-paths?automaton=$A_APPROVE" | jq_r '.held | length')"; N_NONE="$(api GET "/api/autonomous/protected-paths?automaton=nosuch" | jq_r '.held | length')"
[[ "$N_FILTER" == 1 && "$N_NONE" == 0 ]]; ck 2 $? "the automaton filter narrows the list (an unknown id reads empty)" "\`?automaton=$A_APPROVE\` holds $N_FILTER, an unknown id holds $N_NONE"

# ============================================================================
begin_check 4 "Answers: approve, reject and change_spec"
# --- approve ---
Q="$(hold_question "$A_APPROVE")"
CODE="$(answer "$Q" approve)"
wait_until 120 task_done "$A_APPROVE"; TD=$?; [[ $TD == 0 ]] || diag "$A_APPROVE"
wait_until 120 is_done "$A_APPROVE"; AD=$?; [[ $AD == 0 ]] || diag "$A_APPROVE"
[[ "$CODE" == 2?? && $TD == 0 ]]; ck 4 $? "approve: the answer is accepted and the task completes" "answer HTTP $CODE; task status \`$(task_field "$A_APPROVE" '.status')\`"
[[ $AD == 0 && -z "$(task_field "$A_APPROVE" '.protected_path_hold // empty')" && "$(task_field "$A_APPROVE" '.approved_protected_paths | tostring')" == "[\"$RULES_FILE\"]" ]]; ck 4 $? "approve: the Automaton completes, the hold is cleared and the approval covers only the listed path for this task" "Automaton status \`$(prd_status "$A_APPROVE")\`; approved_protected_paths $(task_field "$A_APPROVE" '.approved_protected_paths | tostring'); the Automaton's authorised paths $(prd "$A_APPROVE" | jq -c '.authorized_paths // []')"
[[ "$(wgit "$A_APPROVE" show "HEAD:$RULES_FILE" 2>/dev/null | tail -1)" == "edit by $A_APPROVE" ]]; ck 4 $? "approve: the change is kept in the Automaton branch" "last line of \`$RULES_FILE\` at the branch head: \`$(wgit "$A_APPROVE" show "HEAD:$RULES_FILE" 2>/dev/null | tail -1)\`"
# --- reject ---
Q="$(hold_question "$A_REJECT")"; REJ_BASE="$(task_field "$A_REJECT" '.protected_path_hold.base_commit')"; PRE_HEAD="$(wgit "$A_REJECT" rev-parse HEAD)"
R_BEFORE="$(worker_runs "$A_REJECT")"
CODE="$(answer "$Q" reject)"
wait_until 120 task_done "$A_REJECT"; TD=$?; [[ $TD == 0 ]] || diag "$A_REJECT"
wait_until 120 is_done "$A_REJECT"; AD=$?
POST_HEAD="$(wgit "$A_REJECT" rev-parse HEAD)"; R_AFTER="$(worker_runs "$A_REJECT")"
BASE_CONTENT="$(wgit "$A_REJECT" show "$REJ_BASE:$RULES_FILE" 2>/dev/null)"; NOW_CONTENT="$(wgit "$A_REJECT" show "HEAD:$RULES_FILE" 2>/dev/null)"
[[ "$CODE" == 2?? ]]; ck 4 $? "reject: the answer is accepted" "answer HTTP $CODE"
[[ -n "$REJ_BASE" && "$BASE_CONTENT" == "$NOW_CONTENT" && "$NOW_CONTENT" == "base rules" ]]; ck 4 $? "reject: the protected file is back at its base content" "base commit \`${REJ_BASE:0:9}\`; \`$RULES_FILE\` at the base: \`$BASE_CONTENT\`, at the branch head now: \`$NOW_CONTENT\`"
[[ "$POST_HEAD" != "$PRE_HEAD" && "$(wgit "$A_REJECT" rev-list --count "$PRE_HEAD..HEAD")" -ge 1 && "$(wgit "$A_REJECT" merge-base --is-ancestor "$PRE_HEAD" HEAD && echo yes)" == yes ]]; ck 4 $? "reject: it is a new commit on top of the old history (nothing rewritten)" "head before the answer \`${PRE_HEAD:0:9}\`, after \`${POST_HEAD:0:9}\`; the old head is an ancestor of the new one; new commits: $(wgit "$A_REJECT" log --format=%s "$PRE_HEAD..HEAD" | tr '\n' '|')"
[[ $TD == 0 && "$R_BEFORE" == 1 && "$R_AFTER" == 2 ]]; ck 4 $? "reject: the task ran again (the worker ran a second time) and completed" "worker runs $R_BEFORE then $R_AFTER; task status \`$(task_field "$A_REJECT" '.status')\`; Automaton \`$(prd_status "$A_REJECT")\`"
# --- change_spec ---
Q="$(hold_question "$A_SPEC")"
CODE="$(answer "$Q" change_spec)"; sleep 8
SP_ST="$(task_field "$A_SPEC" '.status')"; SP_AW="$(task_field "$A_SPEC" '.protected_path_hold.awaiting_spec_change')"; SP_RUNS="$(worker_runs "$A_SPEC")"
rep_block "Task JSON after change_spec:" "$(task_json "$A_SPEC" | jq '{id, status, error, protected_path_hold}')"
[[ "$CODE" == 2?? && "$SP_ST" == blocked && "$SP_AW" == true && "$SP_RUNS" == 1 && "$(prd_status "$A_SPEC")" != completed ]]; ck 4 $? "change_spec: the task stays blocked, the hold waits for a spec change and the task did not run again" "answer HTTP $CODE; task status \`$SP_ST\`; awaiting_spec_change \`$SP_AW\`; error \`$(task_field "$A_SPEC" '.error')\`; worker runs $SP_RUNS"
AUDA="$(aud_count autonomous_protected_paths_answered "$A_APPROVE")$(aud_count autonomous_protected_paths_answered "$A_REJECT")$(aud_count autonomous_protected_paths_answered "$A_SPEC")"
[[ "$AUDA" =~ ^[1-9]+$ ]]; ck 4 $? "each answer is in the audit log" "answered events per case (approve, reject, change_spec): $AUDA"

# ============================================================================
begin_check 5 "Authorised path: the same edit passes, clearing it brings the hold back"
AUTH_RES="$(set_auth "$A_AUTH" "[\"$RULES_FILE\"]")"
[[ "$AUTH_RES" == 200* ]]; ck 5 $? "\`POST .../set_authorized_paths\` accepts the list" "$(printf '%s' "$AUTH_RES" | cut -c1-160)"
[[ "$(pp_status | jq -c --arg a "$A_AUTH" '[.authorized[] | select(.automaton_id==$a) | .paths][0]')" == "[\"$RULES_FILE\"]" ]]; ck 5 $? "the authorised list shows in the protected-paths status" "$(pp_status | jq -c '.authorized')"
start_automaton "$A_AUTH" >/dev/null
wait_until 150 is_done "$A_AUTH"; AD=$?; [[ $AD == 0 ]] || diag "$A_AUTH"
NQ="$(task_question_count "$A_AUTH")"
[[ $AD == 0 && "$NQ" == 0 && -z "$(task_field "$A_AUTH" '.protected_path_hold // empty')" && "$(wgit "$A_AUTH" show "HEAD:$RULES_FILE" 2>/dev/null | tail -1)" == "edit by $A_AUTH" ]]; ck 5 $? "with the path authorised the same edit passes with no question and the change is kept" "Automaton \`$(prd_status "$A_AUTH")\`; questions for the task: $NQ; decisions \`$(decisions "$A_AUTH")\`"
CL_OUT="$(set_auth "$A_AUTH" '[]')"
[[ "$CL_OUT" == 200* && "$(prd "$A_AUTH" | jq -c '.authorized_paths // []')" == "[]" ]]; ck 5 $? "an empty list clears the authorisation" "$(prd "$A_AUTH" | jq -c '{authorized_paths}')"
BAD_AUTH="$(set_auth "$A_AUTH" '["/etc/passwd"]')"
[[ "$BAD_AUTH" == 400* ]]; ck 5 $? "an absolute authorised path is refused with a 400" "$BAD_AUTH"
ck 5 $CLEARED_RC "clearing the authorised list brings the hold back (the Automaton had the path authorised, the list was cleared, and the same edit was held)" "$CLEARED_EV"

# ============================================================================
begin_check 6 "Modes: warn, block and off"
set_action warn >/dev/null
start_automaton "$A_WARN" >/dev/null
wait_until 150 is_done "$A_WARN"; WD=$?; [[ $WD == 0 ]] || diag "$A_WARN"
W_DEC="$(decisions "$A_WARN")"; W_AUD="$(aud_count autonomous_protected_paths_warned "$A_WARN")"; W_Q="$(task_question_count "$A_WARN")"
rep_block "Decision log of the warn case:" "$(prd "$A_WARN" | jq -c '[.decisions[]? | select(.kind | startswith("protected_paths")) | {kind, note}]')"
[[ $WD == 0 && "$(task_field "$A_WARN" '.status')" == completed ]]; ck 6 $? "warn: the task and the Automaton complete although a protected file changed" "Automaton \`$(prd_status "$A_WARN")\`, task \`$(task_field "$A_WARN" '.status')\`"
[[ "$W_DEC" == *protected_paths_warned* && "$W_AUD" -ge 1 && "$W_Q" == 0 ]]; ck 6 $? "warn: a decision and an audit event are recorded and no question is raised" "decisions \`$W_DEC\`; audit events $W_AUD; questions for the task $W_Q"
set_action block >/dev/null
start_automaton "$A_BLOCK" >/dev/null
wait_until 150 is_blocked_noq "$A_BLOCK"; BD=$?; [[ $BD == 0 ]] || diag "$A_BLOCK"; sleep 3
B_DEC="$(decisions "$A_BLOCK")"; B_Q="$(task_question_count "$A_BLOCK")"
rep_block "Task JSON of the block case:" "$(task_json "$A_BLOCK" | jq '{id, status, error, protected_path_hold}')"
[[ $BD == 0 && "$B_Q" == 0 && "$B_DEC" == *protected_paths_blocked* && "$(prd_status "$A_BLOCK")" != completed ]]; ck 6 $? "block: the task is blocked with a recorded decision and no question" "task status \`$(task_field "$A_BLOCK" '.status')\`, error \`$(task_field "$A_BLOCK" '.error')\`; decisions \`$B_DEC\`; questions for the task $B_Q"
set_action off >/dev/null
start_automaton "$A_OFF" >/dev/null
wait_until 150 is_done "$A_OFF"; OD=$?; [[ $OD == 0 ]] || diag "$A_OFF"
O_DEC="$(decisions "$A_OFF")"; O_Q="$(task_question_count "$A_OFF")"; O_AUD=$(( $(aud_count autonomous_protected_paths_held "$A_OFF") + $(aud_count autonomous_protected_paths_warned "$A_OFF") + $(aud_count autonomous_protected_paths_blocked "$A_OFF") ))
[[ $OD == 0 && "$O_DEC" != *protected_paths* && "$O_Q" == 0 && "$O_AUD" == 0 ]]; ck 6 $? "off: the task and the Automaton complete and nothing of the check is recorded" "Automaton \`$(prd_status "$A_OFF")\`; decisions \`$O_DEC\`; questions $O_Q; audit events $O_AUD"
set_action hold >/dev/null

# ============================================================================
begin_check 7 "Different names: the project's own rules file and a directory"
NAMES_CODE="$(api_code PUT /api/config "{\"$PA\":[\"$RULES_FILE\",\"$POLICY_DIR/\"]}")"
[[ "$NAMES_CODE" == 200 && "$(pp_status | jq -c '.paths')" == "[\"$RULES_FILE\",\"$POLICY_DIR/\"]" ]]; ck 7 $? "protected_paths is set to the throwaway project's own rules file name and a directory" "HTTP $NAMES_CODE; effective list $(pp_status | jq -c '.paths')"
start_automaton "$A_OTHER" >/dev/null
start_automaton "$A_POLICY" >/dev/null
wait_until 150 is_done "$A_OTHER"; ND=$?; [[ $ND == 0 ]] || diag "$A_OTHER"
wait_until 150 is_held "$A_POLICY"; PD=$?; [[ $PD == 0 ]] || diag "$A_POLICY"
[[ $ND == 0 && "$(task_question_count "$A_OTHER")" == 0 && "$(decisions "$A_OTHER")" != *protected_paths* ]]; ck 7 $? "an edit to \`$OTHER_FILE\`, outside the configured list, passes with no question" "Automaton \`$(prd_status "$A_OTHER")\`; decisions \`$(decisions "$A_OTHER")\`; questions $(task_question_count "$A_OTHER")"
[[ $PD == 0 && "$(task_field "$A_POLICY" '.protected_path_hold.paths | tostring')" == "[\"$POLICY_DIR/policy.md\"]" ]]; ck 7 $? "an edit under the listed directory is held" "task status \`$(task_field "$A_POLICY" '.status')\`, hold paths $(task_field "$A_POLICY" '.protected_path_hold.paths | tostring')"
api PUT /api/config "{\"$PA\":[]}" >/dev/null

# ============================================================================
begin_check 8 "Judge: the base-commit rules review"
JT_FILES="$(ls "$ROOT"/internal/autonomous/rules_review_base_test.go "$ROOT"/internal/autonomous/protected_paths*_test.go 2>/dev/null | sed "s|^$ROOT/||" | tr '\n' ' ')"
JT_COUNT="$(cat "$ROOT"/internal/autonomous/rules_review_base_test.go 2>/dev/null | grep -c '^func Test')"
[[ -f "$ROOT/internal/autonomous/rules_review_base_test.go" && "$JT_COUNT" -gt 0 ]]
ck 8 $? "limitation, not a live result: the judge read of the rules at the base commit (rules_review_source base_commit) is covered by the Go tests with fake judges" "$JT_COUNT test functions in \`internal/autonomous/rules_review_base_test.go\`; test files: $JT_FILES"
rep ""
rep "**Not shown live.** The sandbox has no judge model, so no live judge result is claimed here. The judge's read of the project rules at the base commit (instead of the worktree) is exercised only by the Go tests, which use fake judges. This check records that coverage and nothing more."

# ============================================================================
CUR=0
DAEMON_LOG_TAIL="$(dlog | grep -E 'protected-paths' | cut -c1-200 | tail -12)"

begin_check 9 "Cleanup"
td_stop_all
sleep 1
D_GONE="no"; td_alive "$DPID" || D_GONE="yes"
PORT_CLOSED="no"; td_wait_port_closed "$TLS_PORT" 20 && PORT_CLOSED="yes"
LEFT_OUT="$(env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" check "$LEFT_SNAP" 2>&1)"; LEFT_RC=$?
[[ "$D_GONE" == yes && "$PORT_CLOSED" == yes && "$LEFT_RC" == 0 ]]; ck 9 $? "the sandbox daemon and its sessions are gone and no test daemon was left behind" "daemon gone: $D_GONE; port $TLS_PORT closed: $PORT_CLOSED; leftover check exit $LEFT_RC"
rep ""
rep "- The daemon was ended by \`td_stop_all\` from \`scripts/test-daemon-lib.sh\` (the recorded pid, config path as marker); nothing was ended by name."
rep "- The scratch area (projects, remote, worktrees, data, config, log, worker, state) is removed by the exit handler right after this report is written."

# --- assemble the report ---------------------------------------------------
NCHECK=9
OVERALL=PASS; TOTAL_FAIL=0
for n in $(seq 1 $NCHECK); do TOTAL_FAIL=$(( TOTAL_FAIL + ${CK_FAIL[$n]:-0} )); done
[[ $TOTAL_FAIL -gt 0 ]] && OVERALL=FAIL
for n in $(seq 1 $NCHECK); do [[ "${CK_TOTAL[$n]:-0}" -gt 0 ]] || { OVERALL=FAIL; CK_FAIL[$n]=1; }; done

{
  printf '# Automaton protected-paths hold — live check on a sandbox daemon\n\n'
  printf '**Date:** %s (run started %s, local time)\n' "$(date '+%Y-%m-%d')" "$DAEMON_UP_AT"
  printf '**Version under test:** %s (binary built from this worktree)\n' "${VERSION:-unknown}"
  printf '**Script:** `scripts/protected-paths-live-check.sh` (run as `env -u TMUX -u TMUX_TMPDIR bash scripts/protected-paths-live-check.sh`)\n'
  printf '**Scope:** the protected-paths settings, `GET /api/autonomous/protected-paths` and its MCP, CLI and chat reads, a held task in worktree and direct mode, the approve, reject and change_spec answers, the per-Automaton authorised paths, the warn, block and off modes and project-specific names, against throwaway project repositories whose default branch is `%s`.\n\n' "$PROJECT_DEFAULT_BRANCH"
  printf '## Result\n\n**Overall:** %s\n\n' "$OVERALL"
  printf '| # | Check | Result | Assertions |\n|---|---|---|---|\n'
  for n in $(seq 1 $NCHECK); do
    v=PASS; [[ "${CK_FAIL[$n]:-0}" -gt 0 ]] && v=FAIL
    printf '| %s | %s | %s | %s of %s passed |\n' "$n" "${CK_TITLE[$n]}" "$v" "$(( ${CK_TOTAL[$n]:-0} - ${CK_FAIL[$n]:-0} ))" "${CK_TOTAL[$n]:-0}"
  done
  printf '\n'
  printf '## Setup\n\n'
  printf -- '- **Sandbox daemon only.** Started with `td_start` from `scripts/test-daemon-lib.sh` from a config generated from `testdata/datawatch.yaml` (data folder, ports, loopback host, test token). Added: `autonomous.worktree_base_repo` and `autonomous.worktree_dir`, `autonomous.rules_file` (`%s`, the throwaway project'"'"'s own rules file), `autonomous.questions.enabled`, `autonomous.verification_backend: shell`, `server.allowed_project_roots` and `shell_backend.script_path` (the stand-in worker). The production daemon, its service and its data were not touched, and nothing was ended by name.\n' "$RULES_FILE"
  printf -- '- **Ports.** HTTP %s, TLS %s, MCP %s, DNS %s, webhook %s, notify %s, each probed free just before use.\n' "$PORT" "$TLS_PORT" "$MCP_PORT" "$DNS_PORT" "$HOOK_PORT" "$NTFY_PORT"
  printf -- '- **Session sockets.** The daemon ran with its multiplexer socket directory under the throwaway folder; the script itself issues no multiplexer command.\n'
  printf -- '- **Everything** lives under one temporary folder outside the worktree, removed on every exit path.\n'
  printf -- '- **Throwaway project.** A bare repository whose default branch is `%s`, cloned to the worktree base repository and, for direct mode, to a second clone; `refs/remotes/origin/HEAD` records the default branch and `main` does not exist. It tracks `%s`, `%s` and `%s/`.\n' "$PROJECT_DEFAULT_BRANCH" "$RULES_FILE" "$OTHER_FILE" "$POLICY_DIR"
  printf -- '- **Worker.** The daemon'"'"'s shell backend runs a generated script. It writes a sample file and, by mode, appends a line to the rules file, to the `%s`-named file or to a file under `%s/`. The daemon'"'"'s verification (a one-shot session on the same backend) is answered by the same script with a fixed pass, so no model is involved anywhere.\n' "$OTHER_FILE" "$POLICY_DIR"
  printf '\n'
  printf '```\n%s\n```\n\n' "$DEFAULT_PROBE"
  printf '| Automaton | Worker mode | Kind | Used in |\n|---|---|---|---|\n'
  for row in "${AUTOMATA[@]}"; do
    IFS='|' read -r aid aname amode akind <<< "$row"
    case "$aid" in
      "$A_APPROVE") use="checks 3, 4 (approve)";; "$A_REJECT") use="checks 3, 4 (reject)";; "$A_SPEC") use="checks 3, 4 (change_spec)";;
      "$A_AUTH") use="check 5 (authorised)";; "$A_CLEARED") use="checks 3, 5 (list cleared)";; "$A_WARN") use="check 6 (warn)";;
      "$A_BLOCK") use="check 6 (block)";; "$A_OFF") use="check 6 (off)";; "$A_OTHER") use="check 7 (outside the list)";;
      "$A_POLICY") use="check 7 (inside the list)";; *) use="check 3 (direct mode)";;
    esac
    printf '| `%s` %s | %s | %s | %s |\n' "$aid" "$aname" "$amode" "$akind" "$use"
  done
  printf '\n'
  cat "$S/rep-0.md" 2>/dev/null
  for n in $(seq 1 $NCHECK); do cat "$S/rep-$n.md" 2>/dev/null; done
  printf '\n## Daemon log, the protected-paths lines\n\n```\n%s\n```\n' "$DAEMON_LOG_TAIL"
  printf '\n## What was and was not checked\n\n'
  printf -- '- **Checked live, through the running daemon:** the three settings (round trip, refused words, defaults), the status route and its MCP, CLI and chat reads, the hold and its single question in worktree and direct mode, the three answers, the authorised list (set, used, cleared, refused when absolute), the warn, block and off modes, and project-specific names.\n'
  printf -- '- **Not checked live: the judge.** The sandbox has no judge model. The judge read of the rules at the base commit is covered by the Go tests with fake judges; nothing in this report is a live judge result.\n'
  printf -- '- **The Automata were written into the sandbox store, not created through the create route.** The route only makes drafts and a plan needs a planner model. Approve, run, the answers, every read and every setting go through the REST API of the running daemon; the worktrees and branches were made by the daemon.\n'
  printf -- '- **Reject was shown in worktree mode only.** Direct mode is shown for the hold; the restore code is the same in both modes and is covered by the Go tests.\n'
  printf -- '- **Not shown live:** a restore that fails (the hold kept with a fresh question), a stale or withdrawn question, the PWA card and the held-task banner. They are covered by the Go tests and the PWA tests; this script drives the daemon through REST, MCP, the command line and chat only.\n'
  printf -- '- **Generality.** The projects are throwaway repositories whose default branch is `%s`, with a rules file this script names itself; nothing names the repository this script lives in.\n' "$PROJECT_DEFAULT_BRANCH"
  printf '\nResult: %s\n' "$OVERALL"
} > "$S/report.md"

# No absolute path and no temporary folder name in the tracked report.
HOMEPFX="/ho"; HOMEPFX+="me/"
SCRUB="$S/scrub.sed"
{
  printf 's|%s|<sandbox>|g\n' "$S"
  printf 's|%s|<repo>|g\n' "$ROOT"
  [[ -n "${HOME:-}" ]] && printf 's|%s|<home>|g\n' "$HOME"
  printf 's|/tmp/[^ `"'"'"')]*|<temp>|g\n'
  printf 's|%s[^ `"'"'"')]*|<path>|g\n' "$HOMEPFX"
} > "$SCRUB"
mkdir -p "$(dirname "$REPORT")"
sed -f "$SCRUB" "$S/report.md" > "$REPORT"

say ""
say "Overall: $OVERALL — report: docs/plans/$(basename "$REPORT")"
[[ "$OVERALL" == PASS ]]
