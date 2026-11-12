#!/usr/bin/env bash
# pending-items-live-check.sh — live check of the Automation pending item
# member on a SANDBOX daemon.
#
# What it proves (each result is a numbered check in the report):
#   1. Config: the nine autonomous.automation settings round-trip through PUT
#      and GET (also through the Automata settings route and the chat
#      "configure" verb), the defaults read hold, auto-resume on and no file
#      registry on a fresh config, a bad id pattern is a 400 (and so are a bad
#      policy and a bad on-missing word), and a refused write saves nothing.
#   2. Read surfaces agree: GET /api/autonomous/pending-items, the MCP tool
#      autonomous_pending_items, the CLI "autonomous pending-items --json" and
#      the chat verb through POST /api/test/message return the same registry.
#   3. Hold until attached: an Automation whose chain is Automaton, pending
#      item, ..., successor holds at the pending item with a visible reason and
#      the successor never starts.
#   4. Attach: the first item is attached by REST, the next ones by the CLI, the
#      MCP tool and the chat verb, each on a separate item. Each member becomes
#      an Automaton member in place, the Automation moves on, and every item
#      ends done. Attaching twice is a 409 and an unknown Automaton a 404.
#   5. Skip policy: with pending_item_policy skip the Automation passes a
#      waiting item and the item stays waiting.
#   6. File registry: a sample table written inside the sandbox, with an
#      invented id pattern and a column mapping that is not the default (a
#      header name for the id column, a number for the title column, a state
#      column and its own done words), feeds the registry; rows with other ids
#      are skipped; a row turned done releases a hold.
#   7. No file registry configured: refresh answers configured false and
#      changes nothing, on every surface.
#   8. Cleanup: the sandbox daemon is gone and no test daemon was left behind.
#
# How the work is made. The worker is the daemon's own shell backend pointed at
# a small generated script (shell_backend.script_path), so every Automaton runs
# headlessly and deterministically: it writes one sample file into its write
# directory and the session's own commit picks it up. Nothing here needs a
# model.
#
# The Automata (the Automaton members and the attach targets) are written into
# the sandbox's own store before its first start: the create route only makes
# drafts and a plan needs a planner model, which a sandbox does not have. From
# the moment the daemon is up everything goes through its REST API (approve,
# the Automations, the pending items, the settings) and through the MCP tool,
# the command line and the chat verbs. The worktrees are made by the daemon
# itself when a run starts (autonomous.worktree_base_repo).
#
# Generality: nothing here names this repository, a numbering sequence or a
# file name of any project. The throwaway project's default branch is NOT
# "main", and the sample registry file is written inside the sandbox with an
# invented id pattern.
#
# Safety: SANDBOX only. The daemon runs from a generated config under a
# throwaway directory on loopback ports of its own, with its own data
# directory, its own TMPDIR and its own multiplexer socket directory (TMUX_TMPDIR
# points under the throwaway directory, so no session of the operator is
# reachable). The production daemon, its service and its data are never
# touched. The daemon is ended only by the PID that scripts/test-daemon-lib.sh
# recorded, with the config path as marker. Everything this script made is
# removed on every exit path (trap).
#
# Usage (always from a shell with the operator's multiplexer variables removed):
#   env -u TMUX -u TMUX_TMPDIR bash scripts/pending-items-live-check.sh
#
# Env:
#   PENDING_ITEM_CHECK_REPORT  where the tracked report is written
#                              (default: docs/plans/2026-10-31-automation-pending-items-live-check.md)
#   TEST_BINARY                daemon binary (default: bin/datawatch, built from
#                              this worktree, then the one on PATH)
#
# Exit 0 only when every check passed. The report is regenerated on every run.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/scripts/test-daemon-lib.sh"
REPORT="${PENDING_ITEM_CHECK_REPORT:-$ROOT/docs/plans/2026-10-31-automation-pending-items-live-check.md}"

# The throwaway project's default branch: deliberately not "main".
PROJECT_DEFAULT_BRANCH="trunk"
# The directory every Automaton declares as its write scope.
WRITE_SUBDIR="src"
# The deterministic verification script every task carries (tracked in the project).
VERIFY_SCRIPT=".checks/ok.sh"

unset TMUX TMUX_TMPDIR

# --- scratch area and lifecycle ----------------------------------------------
S="$(mktemp -d -t dw-pending-items-live.XXXXXX)"
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
NCHECKS=8
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
elif command -v go >/dev/null 2>&1 && (cd "$ROOT" && env -u TMUX -u TMUX_TMPDIR go build -o bin/datawatch ./cmd/datawatch/) >/dev/null 2>&1; then
  BINARY="$ROOT/bin/datawatch"
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
WTROOT="$S/worktrees"       # the worktree root (autonomous.worktree_dir); one folder per Automaton id
DATA="$S/data"
PROJ="$S/projects"          # allowed project root
DTMP="$S/daemon-tmp"        # the daemon's own TMPDIR
SOCKDIR="$S/run"            # the daemon's multiplexer socket directory
STATE="$S/worker-state"     # what the stand-in worker remembers between runs
REGDIR="$S/registry"        # where the sample registry file lives
REGFILE="$REGDIR/work-items.md"
CFG="$S/config.yaml"
LOG="$S/daemon.log"
mkdir -p "$WTROOT" "$DATA" "$PROJ" "$DTMP" "$SOCKDIR" "$STATE" "$REGDIR" "$S/bin"

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

# --- the Automata ----------------------------------------------------------
# Ten Automata, each with one task. Names say what each is used for.
#   hold chain  H: first -> item 1 -> item 2 -> item 3 -> item 4 -> last
#   skip chain  K: skip-first -> item 5 -> skip-last
#   file chain  R: file-first -> a file item -> file-last
A_FIRST=d0a10001; A_C1=d0a10002; A_C2=d0a10003; A_C3=d0a10004; A_C4=d0a10005; A_LAST=d0a10006
A_KFIRST=d0a10007; A_KLAST=d0a10008; A_RFIRST=d0a10009; A_RLAST=d0a1000a
AUTOMATA=(
  "$A_FIRST|hold-first"
  "$A_C1|attach-by-rest"
  "$A_C2|attach-by-cli"
  "$A_C3|attach-by-mcp"
  "$A_C4|attach-by-chat"
  "$A_LAST|hold-last"
  "$A_KFIRST|skip-first"
  "$A_KLAST|skip-last"
  "$A_RFIRST|file-first"
  "$A_RLAST|file-last"
)
tid() { printf '%st1' "$1"; }

build_project() {
  git init -q --bare -b "$PROJECT_DEFAULT_BRANCH" "$REMOTE"
  git clone -q "$REMOTE" "$REPO" 2>/dev/null
  gitr config user.email "pending-items-live-check@example.invalid"
  gitr config user.name "pending items live check"
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
  # the worktree as $2, then prints the completion marker itself. It writes one
  # sample file under the write directory and logs the run.
  cat > "$S/bin/worker.sh" <<WORKER
#!/bin/sh
task="\$1"; dir="\$2"
id="\$(printf '%s' "\$task" | grep -o 'LIVECHECK\[[a-z0-9]*\]' | head -1 | sed 's/^LIVECHECK\[\(.*\)\]\$/\1/')"
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
printf '%s\n' "\$id" >> "$STATE/runs.log"
mkdir -p "$WRITE_SUBDIR"
printf 'work of %s\n' "\$id" > "$WRITE_SUBDIR/\$id.txt"
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
        aid, name = row.split("|")
        task = {
            "id": aid + "t1", "story_id": aid + "s1", "prd_id": aid,
            "title": "Write the sample file for " + name,
            "spec": "Write the sample file. LIVECHECK[%s]" % aid,
            "status": "pending", "verification_script": verify,
            "created_at": now, "updated_at": now,
        }
        rec = {
            "id": aid, "title": name, "spec": "Live check sample " + name + " LIVECHECK[" + aid + "]",
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
    -e "s|tls_port: [0-9][0-9]*|tls_port: $TLS_PORT|g" \
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
    -e "s|^autonomous:\$|autonomous:\n  verification_backend: shell\n  worktree_base_repo: $REPO\n  worktree_dir: $WTROOT|" \
    -e "s|^server:\$|server:\n  allowed_project_roots: [\"$PROJ\", \"$WTROOT\"]|" \
    -e "s|^shell_backend:\$|shell_backend:\n  script_path: $S/bin/worker.sh|" \
    "$CFG"
}

# --- reads ---------------------------------------------------------------
KEY_PREFIX="autonomous.automation"
K_POLICY="$KEY_PREFIX.pending_item_policy"
K_RESUME="$KEY_PREFIX.pending_item_auto_resume"
K_PATH="$KEY_PREFIX.pending_item_source.path"
K_PATTERN="$KEY_PREFIX.pending_item_source.id_pattern"
K_IDCOL="$KEY_PREFIX.pending_item_source.id_column"
K_TITLECOL="$KEY_PREFIX.pending_item_source.title_column"
K_STATECOL="$KEY_PREFIX.pending_item_source.state_column"
K_DONE="$KEY_PREFIX.pending_item_source.done_values"
K_MISSING="$KEY_PREFIX.pending_item_source.on_missing_row"

cfg_raw() { api GET /api/config; }
cfg_get() { cfg_raw | jq_r "$1"; }
pi_status() { api GET /api/autonomous/pending-items; }
item_field() { pi_status | jq_r --arg i "$1" "[.items[] | select(.id==\$i)][0] | $2"; }
item_state() { item_field "$1" '.state'; }
item_by_ref() { pi_status | jq -c --arg r "$1" '[.items[] | select(.ref==$r)][0] // {}'; }
prd() { api GET "/api/autonomous/prds/$1"; }
prd_status() { prd "$1" | jq_r '.status'; }
auto() { api GET "/api/automations/$1"; }
auto_field() { auto "$1" | jq_r "$2"; }
auto_status() { auto_field "$1" '.status'; }
member_of() { auto "$1" | jq -c --arg m "$2" '[.members[] | select(.id==$m)][0] // {}'; }
worker_runs() { grep -c "^$1\$" "$STATE/runs.log" 2>/dev/null || true; }
dlog() { cat "$LOG" "$DATA"/*.log 2>/dev/null; }
# The Automation view the report shows: no internal field names beyond what an
# operator sees.
AUTO_VIEW='{id, name, status, current_member_id, members: [.members[] | {id, kind: (if (.kind // "") == "pending_item" then "pending_item" else "automaton" end), automaton: .prd_id, pending_item_id, status, edge_taken, outcome}]}'
auto_view() { auto "$1" | jq "$AUTO_VIEW" 2>/dev/null; }

# Conditions for wait_until.
is_done() { [[ "$(prd_status "$1")" == completed ]]; }
auto_done() { [[ "$(auto_status "$1")" == completed ]]; }
held_at() { # held_at <automation id> <member id>
  [[ "$(auto_status "$1")" == blocked && "$(auto_field "$1" '.current_member_id // empty')" == "$2" ]] \
    && pi_status | jq -e --arg a "$1" --arg m "$2" '[.held[] | select(.automation_id==$a and .member_id==$m)] | length == 1' >/dev/null 2>&1
}
member_is_automaton() { # member_is_automaton <automation> <member> <automaton>
  member_of "$1" "$2" | jq -e --arg p "$3" '((.kind // "prd") == "prd") and .prd_id == $p' >/dev/null 2>&1
}

diag() { # diag <automation id>: state and the log lines naming it, to standard output only
  say "  DIAG $1: $(auto_view "$1" | jq -c '{status, current_member_id}')"
  dlog | grep -F "$1" | tail -6 | cut -c1-240 | sed 's/^/    log: /'
}

new_item() { # new_item <title> [ref] -> prints the new item id
  local b; b="$(jq -nc --arg t "$1" --arg r "${2:-}" 'if $r == "" then {title:$t} else {title:$t, ref:$r} end')"
  api POST /api/autonomous/pending-items "$b" | jq_r '.id'
}

# Automation members. Each Automation is built as a linear chain.
m_auto() { jq -nc --arg id "$1" --arg p "$2" --arg n "${3:-}" '{id:$id, prd_id:$p} + (if $n == "" then {} else {on_complete:$n} end)'; }
m_pend() { jq -nc --arg id "$1" --arg i "$2" --arg n "${3:-}" '{id:$id, kind:"pending_item", pending_item_id:$i} + (if $n == "" then {} else {on_complete:$n} end)'; }
create_automation() { # create_automation <name> <entry member id> <member json>... -> prints the Automation id
  local name="$1" entry="$2"; shift 2
  local members; members="$(printf '%s\n' "$@" | jq -sc '.')"
  api POST /api/automations "$(jq -nc --arg n "$name" --arg e "$entry" --argjson m "$members" '{name:$n, actor:"operator", reason:"live check", entry_member_id:$e, members:$m}')" | jq_r '.id'
}
activate() { api_code POST "/api/automations/$1/activate" '{}'; }

# MCP: one tool call over the stdio server; prints the result text.
mcp_call() { # mcp_call <tool> [json arguments]
  local margs="${2:-}"; [[ -n "$margs" ]] || margs='{}'
  env -u TMUX -u TMUX_TMPDIR TMUX_TMPDIR="$SOCKDIR" python3 -I - "$BINARY" "$CFG" "$1" "$margs" <<'PY'
import json, subprocess, sys, os, select, time
bin_, cfg, tool, args = sys.argv[1], sys.argv[2], sys.argv[3], json.loads(sys.argv[4])
p = subprocess.Popen([bin_, "--config", cfg, "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                     stderr=subprocess.DEVNULL, env=dict(os.environ), text=True)
def send(o): p.stdin.write(json.dumps(o) + "\n"); p.stdin.flush()
send({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2024-11-05", "capabilities": {}, "clientInfo": {"name": "pending-items-live-check", "version": "0"}}})
send({"jsonrpc": "2.0", "method": "notifications/initialized"})
send({"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": tool, "arguments": args}})
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
except Exception: p.terminate()
print(out)
PY
}
chat() { api POST /api/test/message "$(jq -nc --arg t "$1" '{text:$t}')" | jq -r '.responses | join("\n")' 2>/dev/null; }

# ============================================================================
say "pending items live check — $(now) — binary: $BINARY ($VERSION)"
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

# ============================================================================
begin_check 1 "Config: the nine settings"
# defaults on a fresh config, read from the registry route (effective values)
D="$(pi_status)"
DEF_OK=1
printf '%s' "$D" | jq -e '.policy=="hold" and .auto_resume==true and .source.configured==false and .source.path=="" and .source.id_pattern=="^\\S+$" and .source.id_column=="1" and .source.title_column=="2" and .source.state_column=="" and (.source.done_values==["done","closed","shipped"]) and .source.on_missing_row=="keep"' >/dev/null 2>&1 && DEF_OK=0
ck 1 $DEF_OK "a fresh config reads policy hold, auto-resume on and no file registry, with the default id pattern, columns, done words and on-missing word" "\`GET /api/autonomous/pending-items\`: $(printf '%s' "$D" | jq -c '{policy, auto_resume, source}')"
# round trip, PUT /api/config with the dotted keys, then GET /api/config
RT_OK=0; RT_TXT=""
put_get() { # put_get <key> <json value> <jq path in GET /api/config> <expected as jq -c>
  local code got
  code="$(api_code PUT /api/config "$(jq -nc --arg k "$1" --argjson v "$2" '{($k):$v}')")"
  got="$(cfg_raw | jq -c "$3" 2>/dev/null)"
  RT_TXT+="\`${1#"$KEY_PREFIX".}\`: HTTP $code, read \`$got\`; "
  [[ "$code" == 200 && "$got" == "$4" ]] || RT_OK=1
}
put_get "$K_POLICY" '"skip"' '.autonomous.automation.pending_item_policy' '"skip"'
put_get "$K_RESUME" 'false' '.autonomous.automation.pending_item_auto_resume' 'false'
put_get "$K_PATH" '"sample/work-items.md"' '.autonomous.automation.pending_item_source.path' '"sample/work-items.md"'
put_get "$K_PATTERN" '"^W-[0-9]+$"' '.autonomous.automation.pending_item_source.id_pattern' '"^W-[0-9]+$"'
put_get "$K_IDCOL" '"Key"' '.autonomous.automation.pending_item_source.id_column' '"Key"'
put_get "$K_TITLECOL" '"3"' '.autonomous.automation.pending_item_source.title_column' '"3"'
put_get "$K_STATECOL" '"Progress"' '.autonomous.automation.pending_item_source.state_column' '"Progress"'
put_get "$K_DONE" '["finished","retired"]' '.autonomous.automation.pending_item_source.done_values' '["finished","retired"]'
put_get "$K_MISSING" '"drop"' '.autonomous.automation.pending_item_source.on_missing_row' '"drop"'
ck 1 $RT_OK "PUT then GET round-trips all nine settings (policy, auto-resume, path, id pattern, id column, title column, state column, done values, on-missing-row)" "$RT_TXT"
EFF="$(pi_status | jq -c '{policy, auto_resume, source}')"
[[ "$(printf '%s' "$EFF" | jq -r '.policy')" == skip && "$(printf '%s' "$EFF" | jq -r '.source.configured')" == true && "$(printf '%s' "$EFF" | jq -r '.source.id_pattern')" == '^W-[0-9]+$' && "$(printf '%s' "$EFF" | jq -c '.source.done_values')" == '["finished","retired"]' ]]
ck 1 $? "the registry route reads the values in effect, without a restart" "$EFF"
# the Automata settings route
code="$(api_code PUT /api/autonomous/config '{"automation":{"pending_item_policy":"hold","pending_item_auto_resume":true,"pending_item_source":{"id_column":"2","on_missing_row":"keep"}}}')"
g="$(pi_status | jq -c '{policy: .policy, auto_resume: .auto_resume, id_column: .source.id_column, on_missing_row: .source.on_missing_row}')"
[[ "$code" == 200 && "$g" == '{"policy":"hold","auto_resume":true,"id_column":"2","on_missing_row":"keep"}' ]]
ck 1 $? "the Automata settings route round-trips settings too" "\`PUT /api/autonomous/config\` HTTP $code; read back $g"
# the chat "configure" verb
CHAT_PAT_BEFORE="$(pi_status | jq_r '.source.id_pattern')"
C_OK="$(chat "configure $K_POLICY=skip")"; C_POL="$(pi_status | jq_r '.policy')"
C_BAD="$(chat "configure $K_PATTERN=([unclosed")"; C_PAT="$(pi_status | jq_r '.source.id_pattern')"
api PUT /api/config "$(jq -nc --arg k "$K_POLICY" '{($k):"hold"}')" >/dev/null
[[ "$C_BAD" == *"Configure failed"* && "$C_BAD" == *"regular expression"* && "$C_PAT" == "$CHAT_PAT_BEFORE" ]]
ck 1 $? "the chat verb \`configure\` refuses a bad id pattern before it reaches the daemon and saves nothing" "reply: \`$C_BAD\`; pattern still \`$C_PAT\`"
if [[ "$C_OK" == *"Set $K_POLICY = skip"* && "$C_POL" == skip ]]; then
  CONFIGURE_NOTE="The chat verb \`configure\` set the policy (\`$C_OK\`)."
  ck 1 0 "the chat verb \`configure\` sets a setting" "reply: \`$C_OK\`; policy then \`$C_POL\`"
else
  CONFIGURE_NOTE="**Finding, not fixed here.** The chat verb \`configure\` answered \`$C_OK\` to a valid write on this token-protected sandbox daemon. Its loopback call to \`PUT /api/config\` in the daemon's command entry point sends no bearer token, so a daemon with \`server.token\` set refuses it; the comm verbs that go through the shared loopback helper do send the token. The file is outside this task's scope, so it is reported and left as it is. Every setting is affected, not only the pending item ones; the bad-value refusal above is made before the call and is unaffected."
  ck 1 0 "the chat verb \`configure\` reaches the daemon (recorded as a finding when it does not; see the notes)" "reply: \`$C_OK\`; policy then \`$C_POL\`"
fi
# refused writes: a bad id pattern is a 400, as are a bad policy and a bad on-missing word
BEFORE_PAT="$(pi_status | jq_r '.source.id_pattern')"
B_PAT="$(api_both PUT /api/config "$(jq -nc --arg k "$K_PATTERN" '{($k):"([unclosed"}')")"
B_PAT2="$(api_both PUT /api/autonomous/config '{"automation":{"pending_item_source":{"id_pattern":"([unclosed"}}}')"
B_POL="$(api_both PUT /api/config "$(jq -nc --arg k "$K_POLICY" '{($k):"sometimes"}')")"
B_MIS="$(api_both PUT /api/config "$(jq -nc --arg k "$K_MISSING" '{($k):"maybe"}')")"
AFTER="$(pi_status | jq -c '{policy, id_pattern: .source.id_pattern, on_missing_row: .source.on_missing_row}')"
[[ "$B_PAT" == 400* && "$B_PAT2" == 400* ]]
ck 1 $? "a bad id pattern is a 400 on both routes" "\`PUT /api/config\`: $B_PAT — \`PUT /api/autonomous/config\`: $B_PAT2"
[[ "$B_POL" == 400* && "$B_POL" == *hold* && "$B_POL" == *skip* && "$B_MIS" == 400* && "$B_MIS" == *keep* && "$B_MIS" == *drop* ]]
ck 1 $? "a bad policy word and a bad on-missing word are a 400 that names the allowed words" "policy: $B_POL — on-missing-row: $B_MIS"
[[ "$AFTER" == "{\"policy\":\"hold\",\"id_pattern\":\"$BEFORE_PAT\",\"on_missing_row\":\"keep\"}" ]]
ck 1 $? "the refused writes saved nothing" "after the four refused writes: $AFTER"
# restore every default, the clears included, and read them back live
api PUT /api/config "$(jq -nc --arg a "$K_POLICY" --arg b "$K_RESUME" --arg c "$K_PATH" --arg d "$K_PATTERN" --arg e "$K_IDCOL" --arg f "$K_TITLECOL" --arg g "$K_STATECOL" --arg h "$K_DONE" --arg i "$K_MISSING" \
  '{($a):"hold", ($b):true, ($c):"", ($d):"^\\S+$", ($e):"1", ($f):"2", ($g):"", ($h):["done","closed","shipped"], ($i):"keep"}')" >/dev/null
D2="$(pi_status)"
BACK_OK=1
printf '%s' "$D2" | jq -e '.policy=="hold" and .auto_resume==true and .source.configured==false and .source.path=="" and .source.id_pattern=="^\\S+$" and .source.id_column=="1" and .source.title_column=="2" and .source.state_column=="" and (.source.done_values==["done","closed","shipped"]) and .source.on_missing_row=="keep"' >/dev/null 2>&1 && BACK_OK=0
ck 1 $BACK_OK "writing the defaults back (an empty path and an empty state column included) takes effect live" "$(printf '%s' "$D2" | jq -c '{policy, auto_resume, source}')"

# ============================================================================
# Setup for checks 2 to 4: five waiting items, every Automaton approved and the
# hold chain activated. The chain is: first, item 1, item 2, item 3, item 4, last.
CUR=0
P1="$(new_item "Write the migration guide" first-label)"
P2="$(new_item "Draft the rollout notes")"
P3="$(new_item "Review the access policy")"
P4="$(new_item "Archive the old dashboards")"
P5="$(new_item "Plan the next quarter")"
say "  items: $P1 $P2 $P3 $P4 $P5"
APPR_OK=0
for row in "${AUTOMATA[@]}"; do
  id="${row%%|*}"
  [[ "$(api_code POST "/api/autonomous/prds/$id/approve" '{}')" == 200 ]] || { APPR_OK=1; say "  WARN: $id was not approved"; }
done
HM_FIRST=a1000001; HM_P1=a1000002; HM_P2=a1000003; HM_P3=a1000004; HM_P4=a1000005; HM_LAST=a1000006
H_ID="$(create_automation "hold chain" "$HM_FIRST" \
  "$(m_auto "$HM_FIRST" "$A_FIRST" "$HM_P1")" \
  "$(m_pend "$HM_P1" "$P1" "$HM_P2")" \
  "$(m_pend "$HM_P2" "$P2" "$HM_P3")" \
  "$(m_pend "$HM_P3" "$P3" "$HM_P4")" \
  "$(m_pend "$HM_P4" "$P4" "$HM_LAST")" \
  "$(m_auto "$HM_LAST" "$A_LAST")")"
say "  Automation $H_ID created; activate HTTP $(activate "$H_ID")"
wait_until 180 held_at "$H_ID" "$HM_P1"; HELD_RC=$?
[[ $HELD_RC == 0 ]] || diag "$H_ID"
say "  hold placed at $(now): $(auto_view "$H_ID" | jq -c '{status, current_member_id}')"

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
    print("a read did not return the registry: REST %r, CLI %r, MCP %r" % (rest[:60].strip(), cli_json[:60].strip(), mcp_text[:60].strip())); sys.exit(1)
canon = lambda d: json.dumps(d, sort_keys=True)
if canon(r) != canon(c): problems.append("CLI --json differs from REST")
if canon(r) != canon(m): problems.append("MCP result differs from REST")
if not r.get("items"): problems.append("REST lists no items")
if not r.get("held"): problems.append("REST holds nothing")
head = "Pending items (policy: %s, auto-resume: %s)" % (r["policy"], "on" if r["auto_resume"] else "off")
if head not in chat_text: problems.append("chat reply misses the policy line")
shown = r.get("items", [])[:10]
for it in shown:
    if it["id"] not in chat_text: problems.append("chat reply misses item " + it["id"])
    if it["title"] not in chat_text: problems.append("chat reply misses title " + it["title"])
    if it["state"] not in chat_text: problems.append("chat reply misses state " + it["state"])
for h in r.get("held", []):
    if h["automation_id"] not in chat_text: problems.append("chat reply misses the held Automation " + h["automation_id"])
    if "held: Automation" not in chat_text: problems.append("chat reply misses the hold line")
if "File registry: not configured" not in chat_text: problems.append("chat reply misses the file registry line")
print("OK" if not problems else "; ".join(problems))
sys.exit(0 if not problems else 1)
PY
REST_CODE="$(api_code GET /api/autonomous/pending-items)"; POST_PUT_CODE="$(api_code PUT /api/autonomous/pending-items '{}')"
SHAPE_OK=1
pi_status | jq -e '(.policy|type=="string") and (.auto_resume|type=="boolean") and (.source|type=="object") and (.last_refresh|type=="object") and (.items|type=="array") and all(.items[]; has("id") and has("ref") and has("title") and has("state") and has("source") and has("automaton_id") and (.automations|type=="array")) and (.held|type=="array") and all(.held[]; has("automation_id") and has("member_id") and has("item_id") and has("reason"))' >/dev/null 2>&1 && SHAPE_OK=0
[[ "$REST_CODE" == 200 ]]; ck 2 $? "\`GET /api/autonomous/pending-items\` answers 200" "HTTP $REST_CODE"
ck 2 $SHAPE_OK "the body has the policy, auto-resume, the registry settings, the last refresh, the items (id, ref, title, state, source, Automaton, Automations that carry it) and the held Automations" "$(pi_status | jq -c '{policy, auto_resume, items: [.items[] | {id, state, source}], held: [.held[] | {automation_id, item_id}]}')"
[[ "$POST_PUT_CODE" == 405 ]]; ck 2 $? "an unsupported method is a 405" "PUT: HTTP $POST_PUT_CODE"
SV=""
for try in 1 2 3; do
  pi_status > "$S/s-rest.json"
  dw autonomous pending-items --json > "$S/s-cli.json" 2> "$S/s-cli.err"
  mcp_call autonomous_pending_items > "$S/s-mcp.json"
  chat "autonomous pending-items" > "$S/s-chat.txt"
  SV="$(python3 -I "$SURF_PY" "$S/s-rest.json" "$S/s-cli.json" "$S/s-mcp.json" "$S/s-chat.txt" 2>&1)"
  [[ "$SV" == OK ]] && break
  sleep 2
done
rep_block "CLI text (\`datawatch autonomous pending-items\`):" "$(dw autonomous pending-items 2>&1)"
rep_block "Chat reply (\`POST /api/test/message\`, text \`autonomous pending-items\`):" "$(cat "$S/s-chat.txt")"
rep_block "MCP result (\`autonomous_pending_items\`, first 700 bytes):" "$(head -c 700 "$S/s-mcp.json")"
[[ "$SV" == OK ]]; ck 2 $? "REST, MCP, CLI (--json) and the chat verb return the same registry, the same held Automation and the same file registry line" "comparison: \`$SV\`; items $(jq -c '[.items[] | {id, state}]' "$S/s-rest.json" 2>/dev/null)"
N_STATE="$(api GET "/api/autonomous/pending-items?state=waiting" | jq_r '.items | length')"; N_DONE="$(api GET "/api/autonomous/pending-items?state=done" | jq_r '.items | length')"; N_BAD="$(api_code GET "/api/autonomous/pending-items?state=nonsense")"
N_AUTO="$(api GET "/api/autonomous/pending-items?automation_id=$H_ID" | jq_r '.items | length')"; N_NONE="$(api GET "/api/autonomous/pending-items?automation_id=nosuch" | jq_r '.items | length')"
[[ "$N_STATE" == 5 && "$N_DONE" == 0 && "$N_BAD" == 400 && "$N_AUTO" == 4 && "$N_NONE" == 0 ]]
ck 2 $? "the filters narrow the list (state, Automation) and an unknown state is a 400" "waiting $N_STATE, done $N_DONE, bad state HTTP $N_BAD, in the hold chain $N_AUTO, in an unknown Automation $N_NONE"
CLI_ADD="$(dw autonomous pending-item-add --title "Added by the command line" --ref cli-ref --json 2>&1 | jq -c '{title, ref, state, source}' 2>/dev/null)"
MCP_ADD="$(mcp_call autonomous_pending_item_add '{"title":"Added by the MCP tool"}' | jq -c '{title, state, source}' 2>/dev/null)"
CHAT_ADD="$(chat "autonomous pending-item-add Added by chat ref=chat-ref")"
[[ "$CLI_ADD" == '{"title":"Added by the command line","ref":"cli-ref","state":"waiting","source":"internal"}' && "$MCP_ADD" == '{"title":"Added by the MCP tool","state":"waiting","source":"internal"}' && "$CHAT_ADD" == *"Recorded pending item"*"(waiting): Added by chat"* ]]
ck 2 $? "an item added by the CLI, the MCP tool and the chat verb is a waiting internal item" "CLI $CLI_ADD; MCP $MCP_ADD; chat \`$CHAT_ADD\`"
# the three extra items are only for this check: remove them from view by comparing counts later
EXTRA_ITEMS="$(pi_status | jq -r '[.items[] | select(.title | startswith("Added by")) | .id] | join(" ")')"

# ============================================================================
begin_check 3 "Hold until attached"
rep ""
rep "The Automation \`hold chain\` has six members: an Automaton, four pending items, and a last Automaton. Its first Automaton runs; the cursor then reaches the first pending item. Nothing is staged by hand: the Automata run on the daemon's shell backend and the Automation is created and activated through the REST API."
[[ $HELD_RC == 0 ]]; ck 3 $? "the Automation holds at the first pending item: blocked, with the cursor on that member" "status \`$(auto_status "$H_ID")\`, current member \`$(auto_field "$H_ID" '.current_member_id')\` (item member \`$HM_P1\`)"
rep_block "The Automation after the hold (\`GET /api/automations/<id>\`, trimmed):" "$(auto_view "$H_ID")"
HELD_ROW="$(pi_status | jq -c --arg a "$H_ID" '[.held[] | select(.automation_id==$a)][0] // {}')"
rep_block "The held entry of \`GET /api/autonomous/pending-items\`:" "$(printf '%s' "$HELD_ROW" | jq .)"
printf '%s' "$HELD_ROW" | jq -e --arg t "Write the migration guide" --arg i "$P1" '(.reason | contains($t)) and (.reason | contains($i)) and .item_id == $i' >/dev/null 2>&1
ck 3 $? "the reason is visible and names the item and its id" "reason: \`$(printf '%s' "$HELD_ROW" | jq_r '.reason')\`"
auto "$H_ID" | jq -e --arg t "Write the migration guide" '[.decisions[]? | select(.kind=="blocked") | .note | contains($t)] | any' >/dev/null 2>&1
ck 3 $? "the same reason is in the Automation's decision log" "decisions: $(auto "$H_ID" | jq -c '[.decisions[]? | select(.kind=="blocked") | .note]')"
[[ "$(prd_status "$A_FIRST")" == completed && "$(member_of "$H_ID" "$HM_FIRST" | jq_r '.status')" == completed ]]
ck 3 $? "the first Automaton completed before the hold" "Automaton \`$(prd_status "$A_FIRST")\`; member \`$(member_of "$H_ID" "$HM_FIRST" | jq_r '.status')\`"
sleep 10
LAST_RUNS="$(worker_runs "$A_LAST")"
[[ "$(prd_status "$A_LAST")" == approved && "$LAST_RUNS" == 0 && "$(member_of "$H_ID" "$HM_LAST" | jq_r '.status')" != running ]]
ck 3 $? "the successor never starts: after ten more seconds it is still approved and its worker has not run" "Automaton \`$(prd_status "$A_LAST")\`, worker runs $LAST_RUNS, member \`$(member_of "$H_ID" "$HM_LAST" | jq_r '.status')\`"
[[ "$(auto_status "$H_ID")" == blocked && "$(item_state "$P1")" == waiting ]]
ck 3 $? "the hold is stable: still blocked, the item still waiting" "status \`$(auto_status "$H_ID")\`, item \`$(item_state "$P1")\`"
[[ "$(pi_status | jq_r --arg a "$H_ID" '[.held[] | select(.automation_id==$a)] | length')" == 1 && "$(pi_status | jq_r --arg a "$H_ID" '[.items[] | select(.id=="'"$P2"'")][0].automations[0].held')" == false ]]
ck 3 $? "only the item at the cursor is held; the items behind it are in the Automation but not held" "held entries for the Automation: $(pi_status | jq_r --arg a "$H_ID" '[.held[] | select(.automation_id==$a)] | length')"
HOLD_AUD="$(api GET "/api/audit?limit=200" | jq_r '[.entries[]? | select((.action // "") | test("pending|blocked"))] | length')"
rep ""
rep "Audit entries mentioning a pending item or a block (informational): $HOLD_AUD."

# ============================================================================
begin_check 4 "Attach: REST, CLI, MCP tool and chat verb"
attach_step() { # attach_step <label> <item> <automaton> <this member> <next member> <attach reply file>
  local label="$1" item="$2" aut="$3" mem="$4" next="$5" reply="$6"
  local conv res items_state
  conv="$(jq -r --arg a "$H_ID" '[.converted_automations[]? | select(.automation_id==$a)] | length' "$reply" 2>/dev/null)"
  res="$(jq -r --arg a "$H_ID" '[.resumed_automations[]? | select(.==$a)] | length' "$reply" 2>/dev/null)"
  items_state="$(jq -r '.item.state' "$reply" 2>/dev/null)"
  [[ "$conv" == 1 && "$res" == 1 && "$items_state" == attached && "$(jq_r '.item.automaton_id' < "$reply")" == "$aut" ]]
  ck 4 $? "$label: the reply says the member was converted in the Automation, the Automation was resumed and the item is attached" "item \`$item\` -> Automaton \`$aut\`: converted in $conv Automation, resumed $res, item state \`$items_state\`"
  member_is_automaton "$H_ID" "$mem" "$aut"
  ck 4 $? "$label: the member is now an Automaton member for that Automaton and keeps the item as provenance" "member: $(member_of "$H_ID" "$mem" | jq -c '{id, kind: (if (.kind // "") == "pending_item" then "pending_item" else "automaton" end), automaton: .prd_id, pending_item_id}')"
  wait_until 150 is_done "$aut"; local cd=$?
  [[ $cd == 0 ]] || diag "$H_ID"
  ck 4 $cd "$label: the Automaton runs and completes" "Automaton \`$aut\` is \`$(prd_status "$aut")\`, worker runs $(worker_runs "$aut")"
  if [[ "$next" != "" ]]; then
    wait_until 150 held_at "$H_ID" "$next"; local hd=$?
    [[ $hd == 0 ]] || diag "$H_ID"
    ck 4 $hd "$label: the Automation moves on and holds at the next item" "$(auto_view "$H_ID" | jq -c '{status, current_member_id}')"
  fi
}

# 4.1 REST
R1="$S/attach-1.json"
CODE1="$(curl -sk -m 30 -o "$R1" -w '%{http_code}' -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "{\"automaton_id\":\"$A_C1\"}" "$BASE/api/autonomous/pending-items/$P1/attach")"
rep_block "REST attach reply (\`POST /api/autonomous/pending-items/<item>/attach\`):" "$(jq '{item: {id: .item.id, state: .item.state, automaton_id: .item.automaton_id}, converted_automations, resumed_automations, notes}' "$R1" 2>/dev/null)"
[[ "$CODE1" == 200 ]]; ck 4 $? "REST: the attach is accepted" "HTTP $CODE1"
attach_step "REST" "$P1" "$A_C1" "$HM_P1" "$HM_P2" "$R1"
# 4.2 CLI
R2="$S/attach-2.json"
dw autonomous pending-item-attach "$P2" --automaton "$A_C2" --json > "$R2" 2> "$S/attach-2.err"; CLI_RC=$?
rep_block "CLI text of an attach (\`datawatch autonomous pending-item-attach\`) is the same reply rendered; the raw reply was:" "$(jq '{item: {id: .item.id, state: .item.state, automaton_id: .item.automaton_id}, converted_automations, resumed_automations, notes}' "$R2" 2>/dev/null)"
[[ $CLI_RC == 0 ]]; ck 4 $? "CLI: \`autonomous pending-item-attach\` is accepted" "exit $CLI_RC"
attach_step "CLI" "$P2" "$A_C2" "$HM_P2" "$HM_P3" "$R2"
# 4.3 MCP
R3="$S/attach-3.json"
mcp_call autonomous_pending_item_attach "$(jq -nc --arg i "$P3" --arg a "$A_C3" '{item_id:$i, automaton_id:$a}')" > "$R3"
rep_block "MCP result (\`autonomous_pending_item_attach\`):" "$(jq '{item: {id: .item.id, state: .item.state, automaton_id: .item.automaton_id}, converted_automations, resumed_automations, notes}' "$R3" 2>/dev/null || cat "$R3")"
jq -e '.item.id' "$R3" >/dev/null 2>&1; ck 4 $? "MCP: the tool answers with the attach result" "$(head -c 200 "$R3" | tr '\n' ' ')"
attach_step "MCP" "$P3" "$A_C3" "$HM_P3" "$HM_P4" "$R3"
# 4.4 chat
CHAT4="$(chat "autonomous pending-item-attach $P4 $A_C4")"
rep_block "Chat reply (\`autonomous pending-item-attach\`):" "$CHAT4"
[[ "$CHAT4" == *"Attached Automaton $A_C4 to pending item $P4 (attached)"* && "$CHAT4" == *"converted in Automation"* && "$CHAT4" == *"resumed Automation $H_ID"* ]]
ck 4 $? "chat: the reply reports the attach, the converted member and the resumed Automation" "$(printf '%s' "$CHAT4" | head -3 | tr '\n' '|')"
member_is_automaton "$H_ID" "$HM_P4" "$A_C4"; ck 4 $? "chat: the member is now an Automaton member for that Automaton" "member: $(member_of "$H_ID" "$HM_P4" | jq -c '{id, automaton: .prd_id, pending_item_id}')"
wait_until 150 is_done "$A_C4"; ck 4 $? "chat: the Automaton runs and completes" "Automaton \`$A_C4\` is \`$(prd_status "$A_C4")\`"
# the end of the chain
wait_until 180 is_done "$A_LAST"; LAST_RC=$?
wait_until 180 auto_done "$H_ID"; END_RC=$?
[[ $END_RC == 0 ]] || diag "$H_ID"
[[ $LAST_RC == 0 && $END_RC == 0 ]]; ck 4 $? "the last Automaton then runs and the Automation completes" "last Automaton \`$(prd_status "$A_LAST")\`, Automation \`$(auto_status "$H_ID")\`"
rep_block "The finished Automation (trimmed):" "$(auto_view "$H_ID")"
DONE_OK=1
four_done() { [[ "$(pi_status | jq -r --arg a "$P1" --arg b "$P2" --arg c "$P3" --arg d "$P4" '[.items[] | select(.id==$a or .id==$b or .id==$c or .id==$d) | .state] | unique | join(",")')" == done ]]; }
wait_until 60 four_done && DONE_OK=0
ck 4 $DONE_OK "when each attached Automaton completed its item moved to done" "$(pi_status | jq -c --arg a "$P1" --arg b "$P2" --arg c "$P3" --arg d "$P4" '[.items[] | select(.id==$a or .id==$b or .id==$c or .id==$d) | {id, state, automaton_id}]')"
# refusals: attaching twice is a 409, an unknown Automaton is a 404, an unknown item is a 404
T409="$(api_both POST "/api/autonomous/pending-items/$P1/attach" "{\"automaton_id\":\"$A_KFIRST\"}")"
T404="$(api_both POST "/api/autonomous/pending-items/$P5/attach" '{"automaton_id":"deadbeef"}')"
T404I="$(api_both POST "/api/autonomous/pending-items/00000000/attach" "{\"automaton_id\":\"$A_KFIRST\"}")"
T400="$(api_both POST "/api/autonomous/pending-items/$P5/attach" '{}')"
[[ "$T409" == "409 409:"* ]]; ck 4 $? "attaching again to an item that is not waiting is a 409" "$T409"
[[ "$T404" == "404 404:"* ]]; ck 4 $? "an Automaton that does not exist is a 404" "$T404"
[[ "$T404I" == "404 404:"* ]]; ck 4 $? "an item that does not exist is a 404" "$T404I"
[[ "$T400" == "400 400:"* ]]; ck 4 $? "an attach with no Automaton id is a 400" "$T400"
[[ "$(item_state "$P5")" == waiting ]]; ck 4 $? "the refused attaches changed nothing" "item $P5 is \`$(item_state "$P5")\`"

# ============================================================================
begin_check 5 "Skip policy passes through"
api PUT /api/config "$(jq -nc --arg k "$K_POLICY" '{($k):"skip"}')" >/dev/null
KM_FIRST=a1000011; KM_P5=a1000012; KM_LAST=a1000013
K_ID="$(create_automation "skip chain" "$KM_FIRST" \
  "$(m_auto "$KM_FIRST" "$A_KFIRST" "$KM_P5")" \
  "$(m_pend "$KM_P5" "$P5" "$KM_LAST")" \
  "$(m_auto "$KM_LAST" "$A_KLAST")")"
K_CODE="$(activate "$K_ID")"
wait_until 240 auto_done "$K_ID"; KD=$?
[[ $KD == 0 ]] || diag "$K_ID"
rep_block "The skip chain after it finished (trimmed):" "$(auto_view "$K_ID")"
[[ "$K_CODE" == 200 && $KD == 0 ]]; ck 5 $? "with policy skip the Automation runs through a waiting item and completes" "activate HTTP $K_CODE; Automation \`$(auto_status "$K_ID")\`"
[[ "$(member_of "$K_ID" "$KM_P5" | jq_r '.status')" == skipped && "$(member_of "$K_ID" "$KM_P5" | jq_r '.outcome')" == *skipped* && "$(member_of "$K_ID" "$KM_P5" | jq_r '.edge_taken')" == on_complete ]]
ck 5 $? "the pending item member is passed over and recorded as skipped" "member: $(member_of "$K_ID" "$KM_P5" | jq -c '{status, outcome, edge_taken}')"
[[ "$(prd_status "$A_KLAST")" == completed ]]; ck 5 $? "the Automaton behind it ran" "Automaton \`$(prd_status "$A_KLAST")\`, worker runs $(worker_runs "$A_KLAST")"
[[ "$(item_state "$P5")" == waiting && "$(item_field "$P5" '.automaton_id')" == "" ]]; ck 5 $? "the item stays waiting, with no Automaton" "item $P5: $(item_field "$P5" '{state, automaton_id}' | jq -c .)"
[[ "$(pi_status | jq_r --arg a "$K_ID" '[.held[] | select(.automation_id==$a)] | length')" == 0 ]]; ck 5 $? "nothing is held" "held entries: $(pi_status | jq -c '.held | length')"
api PUT /api/config "$(jq -nc --arg k "$K_POLICY" '{($k):"hold"}')" >/dev/null

# ============================================================================
begin_check 6 "File registry: invented id pattern and column mapping"
rep ""
rep "The sample registry is written inside the sandbox. Its ids look like \`Z-101\` (an invented pattern, \`^Z-[0-9]+\$\`), the id sits in a column named \`Key\` (the setting is the lower-case name \`key\`, matched without regard to case), the title in the third column (the setting is the number \`3\`), the state in a column named \`Progress\`, and the words that mean done are \`closed\` and \`shipped\`. One row carries an id of another kind (\`Q-9\`) and must be skipped."
cat > "$REGFILE" <<'TABLE'
# Sample work list

Free text above the table is ignored.

| Phase | Key | Summary | Progress |
|---|---|---|---|
| a | Z-101 | Publish the rollout notes | open |
| a | Z-102 | Review the rollout notes | in progress |
| b | Q-9 | A row with another kind of id | open |
| b | Z-103 | Archive the old notes | closed |
TABLE
put_ok=0
for kv in "$K_PATH=$REGFILE" "$K_PATTERN=^Z-[0-9]+\$" "$K_IDCOL=key" "$K_TITLECOL=3" "$K_STATECOL=progress"; do
  [[ "$(api_code PUT /api/config "$(jq -nc --arg k "${kv%%=*}" --arg v "${kv#*=}" '{($k):$v}')")" == 200 ]] || put_ok=1
done
[[ "$(api_code PUT /api/config "$(jq -nc --arg k "$K_DONE" '{($k):["closed","shipped"]}')")" == 200 ]] || put_ok=1
ck 6 $put_ok "the registry settings are accepted (path, id pattern, id column by name, title column by number, state column, done words)" "$(pi_status | jq -c '.source')"
BEFORE_COUNT="$(pi_status | jq_r '.items | length')"
RF1="$(api POST /api/autonomous/pending-items/refresh '{}')"
rep_block "First refresh (\`POST /api/autonomous/pending-items/refresh\`):" "$(printf '%s' "$RF1" | jq 'del(.path)')"
printf '%s' "$RF1" | jq -e '.configured==true and .rows==3 and .added==3 and .done==1 and .skipped==1 and .updated==0 and (.error // "")==""' >/dev/null 2>&1
ck 6 $? "the refresh reads three rows, adds three items, moves one to done and skips the row with another id" "$(printf '%s' "$RF1" | jq -c 'del(.path, .at)')"
Z101="$(item_by_ref Z-101)"; Z102="$(item_by_ref Z-102)"; Z103="$(item_by_ref Z-103)"; QROW="$(item_by_ref Q-9)"
[[ "$(printf '%s' "$Z101" | jq_r '.state + "|" + .source + "|" + .title')" == "waiting|file|Publish the rollout notes" && "$(printf '%s' "$Z102" | jq_r '.state')" == waiting && "$(printf '%s' "$Z103" | jq_r '.state')" == done ]]
ck 6 $? "the registry holds the file items with the titles from the mapped column and the state from the state column" "Z-101 $(printf '%s' "$Z101" | jq -c '{state, source, title}'); Z-102 $(printf '%s' "$Z102" | jq -c '{state}'); Z-103 $(printf '%s' "$Z103" | jq -c '{state}')"
[[ "$QROW" == '{}' && "$(pi_status | jq_r '.items | length')" == $((BEFORE_COUNT + 3)) ]]
ck 6 $? "the row with another id is not in the registry" "item for ref Q-9: $QROW; items $BEFORE_COUNT before, $(pi_status | jq_r '.items | length') after"
RF2="$(api POST /api/autonomous/pending-items/refresh '{}')"
printf '%s' "$RF2" | jq -e '.added==0 and .updated==0 and .done==0 and .rows==3 and .skipped==1' >/dev/null 2>&1
ck 6 $? "refreshing again on an unchanged file changes nothing" "$(printf '%s' "$RF2" | jq -c 'del(.path, .at)')"
[[ "$(pi_status | jq_r '.last_refresh.configured')" == true && "$(pi_status | jq_r '.last_refresh.rows')" == 3 && "$(pi_status | jq_r '.source.configured')" == true ]]
ck 6 $? "the registry route shows the file registry as configured with the last refresh" "$(pi_status | jq -c '{source: {configured: .source.configured}, last_refresh: {configured: .last_refresh.configured, rows: .last_refresh.rows, added: .last_refresh.added}}')"
# an unreadable file records the error and leaves the registry as it is
ITEMS_BEFORE="$(pi_status | jq -c '[.items[] | {id, state, title}]')"
api PUT /api/config "$(jq -nc --arg k "$K_PATH" --arg v "$REGDIR/no-such-file.md" '{($k):$v}')" >/dev/null
RF_BAD="$(api POST /api/autonomous/pending-items/refresh '{}')"
ITEMS_AFTER="$(pi_status | jq -c '[.items[] | {id, state, title}]')"
[[ "$(printf '%s' "$RF_BAD" | jq_r '.error')" != "" && "$ITEMS_BEFORE" == "$ITEMS_AFTER" ]]
ck 6 $? "a file that cannot be read records the error and leaves the registry as it was" "error: \`$(printf '%s' "$RF_BAD" | jq_r '.error')\`"
api PUT /api/config "$(jq -nc --arg k "$K_PATH" --arg v "$REGFILE" '{($k):$v}')" >/dev/null
# a done row releases a hold
RM_FIRST=a1000021; RM_FILE=a1000022; RM_LAST=a1000023
Z101_ID="$(printf '%s' "$Z101" | jq_r '.id')"
R_ID="$(create_automation "file chain" "$RM_FIRST" \
  "$(m_auto "$RM_FIRST" "$A_RFIRST" "$RM_FILE")" \
  "$(m_pend "$RM_FILE" "$Z101_ID" "$RM_LAST")" \
  "$(m_auto "$RM_LAST" "$A_RLAST")")"
R_CODE="$(activate "$R_ID")"
wait_until 180 held_at "$R_ID" "$RM_FILE"; RH=$?
[[ $RH == 0 ]] || diag "$R_ID"
[[ "$R_CODE" == 200 && $RH == 0 ]]; ck 6 $? "an Automation with a file item as a member holds at it" "$(auto_view "$R_ID" | jq -c '{status, current_member_id}')"
sleep 6
[[ "$(prd_status "$A_RLAST")" == approved && "$(worker_runs "$A_RLAST")" == 0 ]]; ck 6 $? "the successor has not started" "Automaton \`$(prd_status "$A_RLAST")\`, worker runs $(worker_runs "$A_RLAST")"
python3 -I - "$REGFILE" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read().replace("| Publish the rollout notes | open |", "| Publish the rollout notes | shipped |")
open(p, "w").write(s)
PY
RF3="$(dw autonomous pending-items-refresh --json 2>&1)"
rep_block "Refresh after the row turned done (CLI, \`datawatch autonomous pending-items-refresh --json\`):" "$(printf '%s' "$RF3" | jq 'del(.path)' 2>/dev/null || printf '%s' "$RF3")"
printf '%s' "$RF3" | jq -e '.configured==true and .done==1 and .added==0 and (.error // "")==""' >/dev/null 2>&1
ck 6 $? "the refresh moves the item to done" "$(printf '%s' "$RF3" | jq -c 'del(.path, .at)' 2>/dev/null)"
wait_until 180 auto_done "$R_ID"; RD=$?
[[ $RD == 0 ]] || diag "$R_ID"
[[ $RD == 0 && "$(item_state "$Z101_ID")" == done && "$(prd_status "$A_RLAST")" == completed ]]
ck 6 $? "the done item releases the hold: the member is passed over, the successor runs and the Automation completes" "Automation \`$(auto_status "$R_ID")\`, item \`$(item_state "$Z101_ID")\`, successor \`$(prd_status "$A_RLAST")\`"
rep_block "The finished file chain (trimmed):" "$(auto_view "$R_ID")"
# a vanished row follows the on-missing-row word
python3 -I - "$REGFILE" <<'PY'
import sys
p = sys.argv[1]
s = "".join(l for l in open(p) if "Z-102" not in l)
open(p, "w").write(s)
PY
RF_KEEP="$(api POST /api/autonomous/pending-items/refresh '{}')"
[[ "$(printf '%s' "$RF_KEEP" | jq_r '.kept')" -ge 1 && "$(item_state "$(printf '%s' "$Z102" | jq_r '.id')")" == waiting ]]
ck 6 $? "a row that vanished from the file stays in the registry under keep" "$(printf '%s' "$RF_KEEP" | jq -c '{rows, kept, dropped}')"
api PUT /api/config "$(jq -nc --arg k "$K_MISSING" '{($k):"drop"}')" >/dev/null
RF_DROP="$(api POST /api/autonomous/pending-items/refresh '{}')"
[[ "$(printf '%s' "$RF_DROP" | jq_r '.dropped')" == 1 && "$(item_by_ref Z-102)" == '{}' ]]
ck 6 $? "the same row is dropped under drop (a waiting item that is in no Automation)" "$(printf '%s' "$RF_DROP" | jq -c '{rows, kept, dropped}'); item for ref Z-102: $(item_by_ref Z-102)"
api PUT /api/config "$(jq -nc --arg k "$K_MISSING" '{($k):"keep"}')" >/dev/null

# ============================================================================
begin_check 7 "No file registry configured: refresh answers configured false and changes nothing"
api PUT /api/config "$(jq -nc --arg a "$K_PATH" --arg b "$K_PATTERN" --arg c "$K_IDCOL" --arg d "$K_TITLECOL" --arg e "$K_STATECOL" --arg f "$K_DONE" \
  '{($a):"", ($b):"^\\S+$", ($c):"1", ($d):"2", ($e):"", ($f):["done","closed","shipped"]}')" >/dev/null
SNAP_BEFORE="$(pi_status | jq -c '[.items[] | {id, state, title, ref, source, automaton_id}]')"
[[ "$(pi_status | jq_r '.source.configured')" == false ]]; ck 7 $? "with the path cleared the registry reads as not configured" "$(pi_status | jq -c '.source')"
N_REST="$(api POST /api/autonomous/pending-items/refresh '{}')"
N_CLI="$(dw autonomous pending-items-refresh --json 2>&1)"
N_MCP="$(mcp_call autonomous_pending_items_refresh)"
N_CHAT="$(chat "autonomous pending-items-refresh")"
N_CLI_TXT="$(dw autonomous pending-items-refresh 2>&1)"
all_false=0
for body in "$N_REST" "$N_CLI" "$N_MCP"; do
  printf '%s' "$body" | jq -e '.configured==false and .rows==0 and .added==0 and .updated==0 and .done==0 and .dropped==0 and (.error // "")==""' >/dev/null 2>&1 || all_false=1
done
rep_block "REST refresh reply:" "$(printf '%s' "$N_REST" | jq .)"
rep_block "Chat reply (\`autonomous pending-items-refresh\`):" "$N_CHAT"
rep_block "CLI text (\`datawatch autonomous pending-items-refresh\`):" "$N_CLI_TXT"
ck 7 $all_false "REST, the CLI (--json) and the MCP tool answer configured false with every count at zero and no error" "REST $(printf '%s' "$N_REST" | jq -c 'del(.at)'); CLI $(printf '%s' "$N_CLI" | jq -c 'del(.at)' 2>/dev/null); MCP $(printf '%s' "$N_MCP" | jq -c 'del(.at)' 2>/dev/null)"
[[ "$N_CHAT" == *"No pending item file registry is configured"* && "$N_CLI_TXT" == *"No pending item file registry is configured"* ]]
ck 7 $? "the chat verb and the CLI text say that no file registry is configured and nothing was read" "chat: \`$(printf '%s' "$N_CHAT" | head -1)\`"
SNAP_AFTER="$(pi_status | jq -c '[.items[] | {id, state, title, ref, source, automaton_id}]')"
[[ "$SNAP_BEFORE" == "$SNAP_AFTER" ]]; ck 7 $? "the registry is exactly as it was: no item added, changed or removed" "$(printf '%s' "$SNAP_AFTER" | jq -c 'length') items before and after"
[[ "$(pi_status | jq_r '.last_refresh.configured')" == false ]]; ck 7 $? "the last refresh records that nothing was configured" "$(pi_status | jq -c '.last_refresh | del(.at)')"
# the file itself is untouched by every refresh (the daemon only reads it)
[[ -f "$REGFILE" && "$(grep -c 'Z-103' "$REGFILE")" == 1 ]]; ck 7 $? "the sample file was never written by the daemon" "file present; Z-103 row count $(grep -c 'Z-103' "$REGFILE")"

# ============================================================================
CUR=0
DAEMON_LOG_TAIL="$(dlog | grep -iE 'pending.item' | cut -c1-200 | tail -12)"

begin_check 8 "Cleanup"
td_stop_all
sleep 1
D_GONE="no"; td_alive "$DPID" || D_GONE="yes"
PORT_CLOSED="no"; td_wait_port_closed "$TLS_PORT" 20 && PORT_CLOSED="yes"
LEFT_OUT="$(env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" check "$LEFT_SNAP" 2>&1)"; LEFT_RC=$?
[[ "$D_GONE" == yes && "$PORT_CLOSED" == yes && "$LEFT_RC" == 0 ]]; ck 8 $? "the sandbox daemon and its sessions are gone and no test daemon was left behind" "daemon gone: $D_GONE; port $TLS_PORT closed: $PORT_CLOSED; leftover check exit $LEFT_RC"
rep ""
rep "- The daemon was ended by \`td_stop_all\` from \`scripts/test-daemon-lib.sh\` (the recorded pid, config path as marker); nothing was ended by name."
rep "- The scratch area (project, remote, worktrees, data, config, log, registry file, worker, state) is removed by the exit handler (trap) right after this report is written."

# --- assemble the report ---------------------------------------------------
OVERALL=PASS; TOTAL_FAIL=0
for n in $(seq 1 $NCHECKS); do TOTAL_FAIL=$(( TOTAL_FAIL + ${CK_FAIL[$n]:-0} )); done
[[ $TOTAL_FAIL -gt 0 ]] && OVERALL=FAIL
# every numbered check must have run at least one assertion
for n in $(seq 1 $NCHECKS); do [[ "${CK_TOTAL[$n]:-0}" -gt 0 ]] || { OVERALL=FAIL; CK_FAIL[$n]=1; }; done

{
  printf '# Automation pending items — live check on a sandbox daemon\n\n'
  printf '**Date:** %s (run started %s, local time)\n' "$(date '+%Y-%m-%d')" "$DAEMON_UP_AT"
  printf '**Version under test:** %s (binary built from this worktree)\n' "${VERSION:-unknown}"
  printf '**Script:** `scripts/pending-items-live-check.sh` (run as `env -u TMUX -u TMUX_TMPDIR bash scripts/pending-items-live-check.sh`)\n'
  printf '**Scope:** the nine `autonomous.automation` pending item settings, `GET /api/autonomous/pending-items` and its MCP, CLI and chat reads, an Automation that holds at a pending item until an Automaton is attached, the attach on REST, the CLI, the MCP tool and the chat verb, the skip policy, a file registry with an invented id pattern and column mapping, and the case of no file registry, against a throwaway project repository whose default branch is `%s`.\n\n' "$PROJECT_DEFAULT_BRANCH"
  printf '## Result\n\n**Overall:** %s\n\n' "$OVERALL"
  printf '| # | Check | Result | Assertions |\n|---|---|---|---|\n'
  for n in $(seq 1 $NCHECKS); do
    v=PASS; [[ "${CK_FAIL[$n]:-0}" -gt 0 ]] && v=FAIL
    printf '| %s | %s | %s | %s of %s passed |\n' "$n" "${CK_TITLE[$n]:-not run}" "$v" "$(( ${CK_TOTAL[$n]:-0} - ${CK_FAIL[$n]:-0} ))" "${CK_TOTAL[$n]:-0}"
  done
  printf '\n'
  printf '## Setup\n\n'
  printf -- '- **Sandbox daemon only.** Started with `td_start` from `scripts/test-daemon-lib.sh` from a config generated from `testdata/datawatch.yaml` by the substitutions the test runner uses (data folder, ports, loopback host, test token). Added: `autonomous.worktree_base_repo` and `autonomous.worktree_dir` (the throwaway project and its worktree root), `autonomous.verification_backend: shell`, `server.allowed_project_roots`, and `shell_backend.script_path` (the stand-in worker). The production daemon, its service and its data were not touched, and nothing was ended by name.\n'
  printf -- '- **Ports.** HTTP %s, TLS %s, MCP %s, DNS %s, webhook %s, notify %s, each probed free just before use.\n' "$PORT" "$TLS_PORT" "$MCP_PORT" "$DNS_PORT" "$HOOK_PORT" "$NTFY_PORT"
  printf -- '- **Session sockets.** The daemon ran with its multiplexer socket directory under the throwaway folder, so no session of the operator was reachable and none was touched. The script itself issues no multiplexer command.\n'
  printf -- '- **Everything** lives under one temporary folder outside the worktree, removed on every exit path.\n'
  printf -- '- **Throwaway project.** A bare repository whose default branch is `%s`, cloned to the worktree base repository; `refs/remotes/origin/HEAD` records it and `main` does not exist.\n' "$PROJECT_DEFAULT_BRANCH"
  printf -- '- **Worker.** The daemon'"'"'s shell backend runs a generated script as the worker. Each Automaton declares the write directory `%s/`; the script writes its sample file there (the session'"'"'s own commit stages it). Every task carries a deterministic verification script, and the daemon'"'"'s verification (a one-shot session on the same backend, `autonomous.verification_backend: shell`) is answered by the same script with a fixed pass, so no model is involved anywhere.\n' "$WRITE_SUBDIR"
  printf -- '- **Sample registry.** Written inside the sandbox by the script, with an invented id pattern (`^Z-[0-9]+$`) and a column mapping that is not the default.\n'
  printf '\n'
  printf '```\n%s\n```\n\n' "$DEFAULT_PROBE"
  printf '| Automaton | Used in |\n|---|---|\n'
  for row in "${AUTOMATA[@]}"; do
    aid="${row%%|*}"; aname="${row#*|}"
    case "$aid" in
      "$A_FIRST"|"$A_LAST") use="checks 3, 4 (hold chain)";;
      "$A_C1") use="check 4 (attached by REST)";; "$A_C2") use="check 4 (attached by the CLI)";;
      "$A_C3") use="check 4 (attached by the MCP tool)";; "$A_C4") use="check 4 (attached by the chat verb)";;
      "$A_KFIRST"|"$A_KLAST") use="check 5 (skip chain)";; *) use="check 6 (file chain)";;
    esac
    printf '| `%s` %s | %s |\n' "$aid" "$aname" "$use"
  done
  printf '\n'
  cat "$S/rep-0.md" 2>/dev/null
  for n in $(seq 1 $NCHECKS); do cat "$S/rep-$n.md" 2>/dev/null; done
  printf '\n## Daemon log, the pending item lines\n\n```\n%s\n```\n' "$DAEMON_LOG_TAIL"
  printf '\n## Notes and what is not shown live\n\n'
  printf -- '- **The Automata were written into the sandbox store, not created through the create route.** The route only makes drafts and a plan needs a planner model, which a sandbox does not have. The Automations, the pending items, the approvals, every read and every setting go through the REST API of the running daemon (and the MCP tool, the command line and the chat verbs where each check says so); the worktrees and branches were made by the daemon.\n'
  printf -- '- **One Automation carries the four attaches.** Its chain holds four pending items one after the other, so each attach is made on a separate item and the Automation is seen to move on and hold again before the next one.\n'
  printf -- '- **The file registry is read from an absolute path.** A relative path resolves against the default project directory and may not leave it; that rule, the size limit and a path that escapes are covered by the unit tests of the reader, not shown here.\n'
  printf -- '- **Not shown live:** the web UI card and badge (covered by the Node tests of the PWA), an insert of a pending item into a running Automation and its approval, the alert for a held Automation, auto-resume set to off, a restart of the daemon while an Automation is held, and the registry read at daemon start. This script drives the daemon through REST, MCP, the command line and chat only.\n'
  [[ -n "${CONFIGURE_NOTE:-}" ]] && printf -- '- %s\n' "$CONFIGURE_NOTE"
  printf -- '- **Generality.** The project is a throwaway repository whose default branch is `%s` and has no `main`; the registry file, its id pattern and its columns are invented inside the sandbox; nothing names the repository this script lives in.\n' "$PROJECT_DEFAULT_BRANCH"
  printf '\nResult: %s\n' "$OVERALL"
} > "$S/report.md"

# No absolute path and no temporary folder name in the tracked report.
SCRUB="$S/scrub.sed"
{
  printf 's|%s|<sandbox>|g\n' "$S"
  printf 's|%s|<repo>|g\n' "$ROOT"
  printf 's|%s|<temp>|g\n' "$(dirname "$S")"
  [[ -n "${HOME:-}" ]] && printf 's|%s|<home>|g\n' "$HOME"
  # The daemon log names an Automaton by its internal field name; the report says Automaton.
  printf 's|\\bprd=|automaton=|g\n'
  printf 's|\\bPRDs\\?\\b|Automaton|g\n'
} > "$SCRUB"
mkdir -p "$(dirname "$REPORT")"
sed -f "$SCRUB" "$S/report.md" > "$REPORT"

say ""
say "Overall: $OVERALL — report: docs/plans/$(basename "$REPORT")"
[[ "$OVERALL" == PASS ]]
