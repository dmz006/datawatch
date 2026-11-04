#!/usr/bin/env bash
# model-routing-live-check.sh — live check of model routing on a SANDBOX
# daemon, against two stand-in model servers.
#
# What it proves (each step is a lettered result in the report):
#   (a) config round trip: the six autonomous.model_routing keys read back their
#       defaults, each can be set and read back, a bad value is refused with 400
#       and a valid key sent in the same request is not applied;
#   (b) the served models of both nodes read the same on REST, MCP, the CLI and
#       the comm channel;
#   (c) pin-time refusal: a model neither node serves is refused with an answer
#       that names the model and both nodes; a model one node serves is accepted;
#   (d) warn mode: the unserved model is applied, a model_not_served_warning
#       decision is recorded and an alert exists; alert_on_refusal false
#       suppresses the alert;
#   (e) node pin: a node that does not serve the task's model is refused naming
#       node and model, the serving node is accepted and shows as pinned_node on
#       the task, clearing the pin removes it, an unregistered node is refused;
#   (f) cache age: inside the age a read shows the old list, refresh=true shows
#       the new one, and with the age at 0 the next plain read is fresh;
#   (g) the verifier following the worker's node needs a running worker, which
#       the sandbox cannot drive; the report says so and names the Go test that
#       covers it. It is not exercised live.
#
# How the nodes are made: two tiny loopback HTTP servers (python3 -I, written
# inline below) answer GET /api/tags with the list held in a file the check
# rewrites at runtime. They are registered as two compute nodes through the REST
# API, and one throwaway LLM entry hosts a local engine on both. A throwaway
# Automaton with one story and one task is created through the API.
#
# Safety: SANDBOX only. The daemon runs from a generated config under
# .datawatch/live-check/ (git-ignored) on loopback ports, with its own data
# directory and its own TMPDIR. The production daemon, its service and its data
# are never touched. The daemon and the stand-ins are ended only by the PID that
# scripts/test-daemon-lib.sh recorded, with their config or list file as marker.
# Everything this script made is removed on every exit path.
#
# Usage (always from a shell with the operator's tmux variables removed):
#   env -u TMUX -u TMUX_TMPDIR bash scripts/model-routing-live-check.sh
#
# Env:
#   MODEL_ROUTING_CHECK_REPORT   where the tracked report is written
#                                (default: docs/plans/2026-10-31-model-routing-live-check.md)
#
# Exit 0 only when every exercised step passed. The report is regenerated on
# every run.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/scripts/test-daemon-lib.sh"
REPORT="${MODEL_ROUTING_CHECK_REPORT:-$ROOT/docs/plans/2026-10-31-model-routing-live-check.md}"
case "$REPORT" in /*) ;; *) REPORT="$ROOT/$REPORT" ;; esac

unset TMUX TMUX_TMPDIR

# --- scratch area and lifecycle ----------------------------------------------
LC="$ROOT/.datawatch/live-check"
S="$LC/run"
rm -rf "$S"
mkdir -p "$S" "$LC/bin"
export TD_STATE_DIR="$S/tdstate"
REPORT_BUF="$S/report.buf"
: > "$REPORT_BUF"
# shellcheck source=/dev/null
source "$LIB"

REAL_TMUX="$(command -v tmux 2>/dev/null || true)"
TMUX_SOCK="$S/tmux.sock"
CREATED=()   # Automaton ids this run made
NODE_A="live-node-a"
NODE_B="live-node-b"
LLM_NAME="aa-live-check-engine"
MODEL_A="live-a:1b"          # served by node A only
MODEL_B="live-b:1b"          # served by node B only
MODEL_SHARED="live-shared:1b" # served by both
PIN_A="ollama/$MODEL_A"
PIN_NONE="ollama/live-none:1b"
PIN_NONE2="ollama/live-none-two:1b"
ALERT_TITLE="Automaton model not served by any node"

api() { # api <METHOD> <path> [json body] — body to stdout
  local m="$1" p="$2" b="${3:-}"
  if [[ -n "$b" ]]; then
    curl -sk -m 60 -X "$m" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$b" "$BASE$p"
  else
    curl -sk -m 60 -X "$m" -H "Authorization: Bearer $TOKEN" "$BASE$p"
  fi
}
api_both() { # api_both <METHOD> <path> <json body> — "<status> <body>" on one line
  local m="$1" p="$2" b="${3:-}" out
  out="$(curl -sk -m 60 -w '\n%{http_code}' -X "$m" -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' ${b:+-d "$b"} "$BASE$p")"
  printf '%s %s' "${out##*$'\n'}" "$(printf '%s' "${out%$'\n'*}" | tr '\n' ' ')"
}
jq_r() { jq -r "$@" 2>/dev/null; }

drop_automaton() { # drop_automaton <id> — cancel, end its sessions, delete it
  local id="$1"
  api POST "/api/autonomous/prds/$id/cancel" '{}' >/dev/null 2>&1 || true
  drop_sessions
  api DELETE "/api/autonomous/prds/$id?hard=true" >/dev/null 2>&1 || true
}
drop_sessions() { # end and delete every session the sandbox daemon holds
  local ids sid
  ids="$(api GET /api/sessions | jq -r '(if type=="array" then . else .sessions end)[]? | (.full_id // .id)' 2>/dev/null)"
  for sid in $ids; do
    api POST /api/sessions/kill "{\"id\":\"$sid\"}" >/dev/null 2>&1 || true
    api POST /api/sessions/delete "{\"id\":\"$sid\"}" >/dev/null 2>&1 || true
  done
}
drop_registrations() { # remove the throwaway LLM entry and the two nodes
  api DELETE "/api/llms/$LLM_NAME" >/dev/null 2>&1 || true
  api DELETE "/api/compute/nodes/$NODE_A" >/dev/null 2>&1 || true
  api DELETE "/api/compute/nodes/$NODE_B" >/dev/null 2>&1 || true
}

pre_stop_cleanup() {
  # Runs on every exit path while the daemon is still alive: remove what the
  # check created through the daemon's own API.
  local id
  [[ -n "${BASE:-}" ]] || return 0
  for id in "${CREATED[@]:-}"; do
    [[ -n "$id" ]] && drop_automaton "$id"
  done
  drop_sessions
  drop_registrations
}
extra_cleanup() {
  # Runs AFTER the daemon and the stand-ins are gone: remove the scratch area.
  rm -rf "$S" "$LC/bin" 2>/dev/null || true
  rmdir "$LC" 2>/dev/null || true
}
# td_install_traps (scripts/test-daemon-lib.sh) installs the EXIT, INT, TERM and HUP
# trap that runs the two hooks below on every exit path: first the API cleanup
# while the daemon is alive, then the end of the recorded PIDs, then the scratch removal.
TD_PRE_STOP_CLEANUP=pre_stop_cleanup
TD_EXTRA_CLEANUP=extra_cleanup
td_install_traps "$@"

# --- result bookkeeping --------------------------------------------------------
PASS=0
FAIL=0
RESULTS=()      # "letter|verdict|title|summary"
STEP_OK=1
STEP_NOTES=()
say() { printf '%s\n' "$*"; }
rep() { printf '%s\n' "$*" >> "$REPORT_BUF"; }
rep_block() { # rep_block <label> <text>: fenced observed output, bounded
  rep ""
  rep "$1"
  rep '```'
  printf '%s\n' "$2" | head -c 3500 >> "$REPORT_BUF"
  printf '\n' >> "$REPORT_BUF"
  rep '```'
}
now() { date '+%H:%M:%S'; }
section() { say ""; say "== $* =="; rep ""; rep "#### $*"; }
chk() { # chk <description> <0|1 ok> [evidence]  — one sub-check of the current step
  local desc="$1" ok="$2" ev="${3:-}"
  if [[ "$ok" == 1 ]]; then
    say "    ok      $desc"
    rep "- ok — $desc"
  else
    say "    FAILED  $desc"
    rep "- NOT MET — $desc"
    STEP_OK=0
    STEP_NOTES+=("$desc")
  fi
  if [[ -n "$ev" ]]; then rep "  - $ev"; fi
}
step_end() { # step_end <letter> <title> <summary when passed>
  local letter="$1" title="$2" summary="$3" v=PASS
  if [[ "$STEP_OK" != 1 ]]; then
    v=FAIL
    summary="not met: $(printf '%s; ' "${STEP_NOTES[@]}" | sed 's/; $//')"
    FAIL=$((FAIL + 1))
  else
    PASS=$((PASS + 1))
  fi
  RESULTS+=("$letter|$v|$title|$summary")
  say "  [$letter] $v — $title"
  STEP_OK=1
  STEP_NOTES=()
}
b01() { [[ "$1" == "$2" ]] && echo 1 || echo 0; } # equal -> 1
has() { [[ "$1" == *"$2"* ]] && echo 1 || echo 0; } # contains -> 1
code_of() { printf '%s' "${1%% *}"; }
body_of() { printf '%s' "${1#* }"; }

write_report() { # write_report <overall PASS|FAIL> — assemble the tracked report
  local overall="$1" n=${#RESULTS[@]} r letter v title summary
  mkdir -p "$(dirname "$REPORT")"
  {
    printf '# Model routing — sandbox live check\n\n'
    printf 'Date: %s · Branch: `%s` (tree at commit `%s`)\n\n' "$(date '+%Y-%m-%d')" "$BRANCH" "$COMMIT"
    printf '**Sandbox daemon: `datawatch v%s`** (`GET /api/health` read `"version":"%s"`), built from this worktree with\n' "${DAEMON_VERSION:-unknown}" "${DAEMON_VERSION:-unknown}"
    printf '`go build -o .datawatch/live-check/bin/datawatch ./cmd/datawatch/`. Host: `%s`, %s. It ran from its own config and\n' "$HOSTNAME_TXT" "$KERNEL_TXT"
    printf 'data directory under `.datawatch/live-check/` (git-ignored, deleted at the end). The production daemon, its\n'
    printf 'service unit and its ports were never touched.\n\n'
    printf '**Sandbox ports:** HTTP %s, TLS %s, MCP %s, DNS %s, webhook %s, notify %s, planner stand-in %s, model stand-in A %s, model stand-in B %s.\n\n' \
      "${PORT:-?}" "${TLS_PORT:-?}" "${MCP_PORT:-?}" "${DNS_PORT:-?}" "${HOOK_PORT:-?}" "${NTFY_PORT:-?}" "${PLAN_PORT:-?}" "${SA_PORT:-?}" "${SB_PORT:-?}"
    printf 'Script: `scripts/model-routing-live-check.sh`, run as `env -u TMUX -u TMUX_TMPDIR bash scripts/model-routing-live-check.sh`. It\n'
    printf 'regenerates this report on every run. Nothing was ended by name, only by the PID the lifecycle helper recorded.\n\n'
    printf '## Results\n\nOne line per step. A step passes only when every check under it in the evidence is met.\n\n'
    for ((i = 0; i < n; i++)); do
      IFS='|' read -r letter v title summary <<< "${RESULTS[$i]}"
      printf -- '- (%s) %s — Result: %s — %s\n' "$letter" "$title" "$v" "$summary"
    done
    printf '\n**Steps:** %d passed, %d failed, 1 not exercised live (g).\n\n' "$PASS" "$FAIL"
    printf '**Cleanup result:** %s\n\n' "${CLEANUP_TXT:-not reached}"
    printf '## Evidence\n'
    cat "$REPORT_BUF"
    printf '\n\nResult: %s\n' "$overall"
  } | sed -e "s|$S|.datawatch/live-check/run|g" -e "s|$ROOT|.|g" > "$REPORT"
}

die_report() { # die_report <message> — record a setup failure and stop
  RESULTS+=("setup|FAIL|Sandbox setup|$1")
  FAIL=$((FAIL + 1))
  rep ""
  rep "#### Setup failed"
  rep ""
  rep "$1"
  write_report FAIL
  say "FAILED: $1 — report: $REPORT"
  exit 1
}

# --- prerequisites ---------------------------------------------------------
for t in git curl python3 jq go; do
  command -v "$t" >/dev/null 2>&1 || { say "SKIP: $t is not on PATH"; exit 0; }
done
[[ -n "$REAL_TMUX" ]] || { say "SKIP: tmux is not on PATH"; exit 0; }

BRANCH="$(git -C "$ROOT" rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
COMMIT="$(git -C "$ROOT" rev-parse --short=9 HEAD 2>/dev/null || echo unknown)"
HOSTNAME_TXT="$(hostname 2>/dev/null || echo unknown)"
KERNEL_TXT="$(uname -sr 2>/dev/null || echo unknown)"

free_port() {
  python3 -I -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}
PORT="$(free_port)"; TLS_PORT="$(free_port)"; MCP_PORT="$(free_port)"
DNS_PORT="$(free_port)"; HOOK_PORT="$(free_port)"; NTFY_PORT="$(free_port)"; PLAN_PORT="$(free_port)"
SA_PORT="$(free_port)"; SB_PORT="$(free_port)"
TOKEN="dw-test-token-12345"
BASE="https://127.0.0.1:$TLS_PORT"

DATA="$S/data"
PROJ="$S/projects"          # allowed project root
REPO="$PROJ/throwaway"      # the throwaway git project
CFG="$S/config.yaml"
LOG="$S/daemon.log"
BIN="$LC/bin/datawatch"
LAUNCHER="$S/launcher/tmux"
LIST_A="$S/standin-a.models"
LIST_B="$S/standin-b.models"
mkdir -p "$DATA" "$PROJ" "$S/bin" "$S/launcher" "$REPO"

dw() { env -u TMUX -u TMUX_TMPDIR "$BIN" --config "$CFG" "$@"; }
await() { # await <timeout-seconds> <command...> — poll every 0.25 s
  local t="$1"; shift
  local end=$((SECONDS + t))
  while ((SECONDS < end)); do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 0.25
  done
  return 1
}

say "model routing live check — $(now)"
rep ""
rep "Scope: the \`autonomous.model_routing\` config keys on \`GET\`/\`PUT /api/config\`, \`GET /api/compute/served-models\` and its MCP, CLI and comm channel reads, the pin-time refusal and warning on \`set_task_llm\`, the node pin on \`set_task_node\`, the served-models cache age, and the alert and decision records, all against two stand-in model servers registered as compute nodes."

LEFT_SNAP="$S/leftover.snap"
env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" snapshot "$LEFT_SNAP" >/dev/null 2>&1 || true

# --- build the daemon from this tree -------------------------------------------
say "building the daemon from this tree"
BUILD_OUT="$(cd "$ROOT" && env -u TMUX -u TMUX_TMPDIR go build -o "$BIN" ./cmd/datawatch/ 2>&1)" \
  || die_report "go build failed: $(printf '%s' "$BUILD_OUT" | head -c 300)"

# --- the throwaway project, shims and config --------------------------------------
git -C "$REPO" init -q -b trunk
git -C "$REPO" config user.email "model-routing-live-check@example.invalid"
git -C "$REPO" config user.name "model routing live check"
printf 'throwaway project for the model routing live check\n' > "$REPO/README.md"
git -C "$REPO" add README.md
git -C "$REPO" commit -q -m "Initial commit"

# A tmux shim first on the daemon's PATH pins any tmux call the daemon makes to
# a private socket, so the operator's tmux server is never reachable from it.
# The check starts no worker, so no session is expected.
ln -sf "$REAL_TMUX" "$LAUNCHER"
cat > "$S/bin/tmux" <<SHIM
#!/bin/sh
case "\$1" in -V) exec "$REAL_TMUX" -V;; esac
exec "$LAUNCHER" -S "$TMUX_SOCK" "\$@"
SHIM
chmod +x "$S/bin/tmux"

# The planner stand-in: answers the daemon's planning call with a fixed plan so
# an Automaton can be decomposed without a model. The check replaces that plan
# with its own story and task right after.
cat > "$S/planner.py" <<'PY'
import json, sys
from http.server import BaseHTTPRequestHandler, HTTPServer
PLAN = {"title": "Model routing live check", "stories": [{
    "title": "Placeholder story", "description": "Replaced by the live check.", "files": [], "depends_on": [],
    "verification_script": "", "files_generated": [],
    "tasks": [{"title": "Placeholder task", "spec": "Placeholder task, replaced by the live check.",
               "files": [], "depends_on": [], "verification_script": "", "files_generated": []}]}]}
class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_POST(self):
        n = int(self.headers.get("Content-Length", "0") or 0)
        self.rfile.read(n)
        body = json.dumps({"model": "stand-in", "response": json.dumps(PLAN), "done": True}).encode()
        self.send_response(200); self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
    def do_GET(self):
        body = b'{"models":[]}'
        self.send_response(200); self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
HTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
PY

# The model stand-in: answers GET /api/tags with the JSON list of model names
# held in the file given as the second argument. The file is read on every
# request, so the check changes what the node serves at runtime by rewriting it.
cat > "$S/standin.py" <<'PY'
import json, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
LIST_FILE = sys.argv[2]
class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        if self.path.split("?")[0] == "/api/tags":
            try:
                with open(LIST_FILE) as f: names = json.load(f)
            except Exception:
                names = []
            body = json.dumps({"models": [{"name": n, "model": n} for n in names]}).encode()
            code = 200
        else:
            body, code = b"{}", 404
        self.send_response(code); self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
PY
set_list() { # set_list <A|B> <model>... — change what a stand-in serves
  local which="$1" f; shift
  [[ "$which" == A ]] && f="$LIST_A" || f="$LIST_B"
  printf '%s\n' "$@" | jq -R . | jq -sc . > "$f"
}
set_list A "$MODEL_A" "$MODEL_SHARED"
set_list B "$MODEL_B" "$MODEL_SHARED"

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
  -e "s|http://local-node:11434|http://127.0.0.1:$PLAN_PORT|g" \
  -e "s|http://localhost:11434|http://127.0.0.1:$PLAN_PORT|g" \
  "$ROOT/testdata/datawatch.yaml" > "$CFG"
sed -i -e "s|^server:\$|server:\n  allowed_project_roots: [\"$PROJ\"]|" "$CFG"

rep ""
rep "### Setup"
rep ""
rep "- **Sandbox only.** Config generated from \`testdata/datawatch.yaml\` by the substitutions the test runner uses (data folder, loopback ports, test token), plus \`server.allowed_project_roots\` (one folder). No \`autonomous.model_routing\` key is set in the file, so step (a) reads the built-in defaults."
rep "- **Ports** (probed free just before use): HTTP $PORT, TLS $TLS_PORT, MCP $MCP_PORT, DNS $DNS_PORT, webhook $HOOK_PORT, notify $NTFY_PORT, planner stand-in $PLAN_PORT, model stand-in A $SA_PORT, model stand-in B $SB_PORT. Port 8080 and the production TLS port were not used."
rep "- **Model stand-ins.** Two inline \`python3 -I\` servers on loopback, each recorded by PID like the daemon. \`GET /api/tags\` returns the names held in a list file the check rewrites at runtime. Node A starts serving \`$MODEL_A\` and \`$MODEL_SHARED\`; node B starts serving \`$MODEL_B\` and \`$MODEL_SHARED\`."
rep "- **Registered through the API.** Two compute nodes of kind \`ollama\` (\`$NODE_A\` at stand-in A, \`$NODE_B\` at stand-in B) and one throwaway LLM entry \`$LLM_NAME\` of kind \`ollama\` listing both as \`compute_nodes\`, so a local-engine model is checked against exactly these two nodes. All three are deleted through the API at the end."
rep "- **Planner stand-in.** A small HTTP server (\`planner.py\`) answers the daemon's planning call with a fixed one-task plan so an Automaton can be planned without a model; the check replaces that plan with its own story and task. The sandbox daemon also links its own model node to that address; the LLM entry's node list keeps it out of every check."
rep "- **Throwaway project.** A git repository (\`.datawatch/live-check/run/projects/throwaway\`, default branch \`trunk\`) with one commit. No worker is started."

# --- start the stand-ins and the sandbox daemon ----------------------------------
python3 -I "$S/planner.py" "$PLAN_PORT" >/dev/null 2>&1 &
PLAN_PID=$!
td_track_pid "$PLAN_PID" "$S/planner.py"
python3 -I "$S/standin.py" "$SA_PORT" "$LIST_A" >/dev/null 2>&1 &
SA_PID=$!
td_track_pid "$SA_PID" "$LIST_A"
python3 -I "$S/standin.py" "$SB_PORT" "$LIST_B" >/dev/null 2>&1 &
SB_PID=$!
td_track_pid "$SB_PID" "$LIST_B"

export PATH="$S/bin:$PATH" TMPDIR="$S"
if ! td_start "$BIN" "$CFG" "$LOG"; then
  die_report "the sandbox daemon did not stay up; log tail: $(tail -5 "$LOG" 2>/dev/null | tr '\n' ' ')"
fi
DPID="$TD_LAST_PID"
td_set_api "$DPID" "$BASE" "$TOKEN"
if ! await 90 curl -sk -m 3 -o /dev/null -f "$BASE/api/health"; then
  die_report "the sandbox daemon did not answer /api/health; log tail: $(tail -5 "$LOG" 2>/dev/null | tr '\n' ' ')"
fi
DAEMON_VERSION="$(api GET /api/health | jq_r '.version')"
say "  daemon pid $DPID, version $DAEMON_VERSION, healthy at $(now)"
rep ""
rep "Sandbox daemon pid \`$DPID\` (recorded by the lifecycle helper) was healthy at $(now); planner stand-in pid \`$PLAN_PID\`, model stand-ins pid \`$SA_PID\` and \`$SB_PID\`."

# Register the two nodes and the LLM entry through the API.
REG_A="$(api_both POST /api/compute/nodes "$(jq -nc --arg n "$NODE_A" --arg a "http://127.0.0.1:$SA_PORT" '{name:$n,kind:"ollama",address:$a}')")"
REG_B="$(api_both POST /api/compute/nodes "$(jq -nc --arg n "$NODE_B" --arg a "http://127.0.0.1:$SB_PORT" '{name:$n,kind:"ollama",address:$a}')")"
REG_L="$(api_both POST /api/llms "$(jq -nc --arg n "$LLM_NAME" --arg a "$NODE_A" --arg b "$NODE_B" --arg ma "$MODEL_A" --arg mb "$MODEL_B" \
  '{name:$n,kind:"ollama",compute_nodes:[$a,$b],models:[{node:$a,model:$ma},{node:$b,model:$mb}]}')")"
rep ""
rep "Registration answers: node A \`$(code_of "$REG_A")\`, node B \`$(code_of "$REG_B")\`, LLM entry \`$(code_of "$REG_L")\`."
if [[ "$(code_of "$REG_A")" != 200 || "$(code_of "$REG_B")" != 200 || "$(code_of "$REG_L")" != 200 ]]; then
  die_report "registering the sandbox nodes failed: A: $REG_A; B: $REG_B; LLM: $REG_L"
fi

# --- helpers over the Automaton API -----------------------------------------------
prd_get() { api GET "/api/autonomous/prds/$1"; }
prd_status() { prd_get "$1" | jq_r '.status'; }
prd_is() { [[ "$(prd_status "$1")" == "$2" ]]; }
tfield() { # tfield <automaton> <task> <jq expr over the task>
  prd_get "$1" | jq -r --arg t "$2" "[.stories[].tasks[] | select(.id==\$t)][0] | $3" 2>/dev/null
}
dec_count() { # dec_count <automaton> <kind>
  prd_get "$1" | jq -r --arg k "$2" '[.decisions[]? | select(.kind==$k)] | length' 2>/dev/null
}
alerts_named() { # the alerts that carry the model-not-served title, as a JSON array
  api GET /api/alerts | jq -c --arg t "$ALERT_TITLE" '[(if type=="array" then . else .alerts end)[]? | select(((.title // "") + " " + (.message // "") + " " + (.body // "")) | contains($t))]' 2>/dev/null
}
alerts_count() { alerts_named | jq -r 'length' 2>/dev/null; }

# new_automaton <title> <spec> — create, plan, replace the plan with the
# check's own story and task. Sets AID and TID. It never approves or runs it.
new_automaton() {
  local title="$1" spec="$2" body r sid olds i
  body="$(jq -nc --arg t "$title" --arg d "$REPO" '{spec:("Model routing live check: "+$t),title:$t,project_dir:$d,backend:"ollama"}')"
  AID="$(api POST /api/autonomous/prds "$body" | jq_r '.id')"
  TID=""
  [[ -n "$AID" && "$AID" != null ]] || return 1
  CREATED+=("$AID")
  api POST "/api/autonomous/prds/$AID/decompose" '{}' >/dev/null
  if ! await 90 prd_is "$AID" needs_review; then
    NEW_DIAG="decompose status: $(api GET "/api/autonomous/prds/$AID/decompose/status" | head -c 400)"
    return 2
  fi
  r="$(api POST "/api/autonomous/prds/$AID/add_story" '{"title":"Live check story","description":"The task of the live check."}')"
  sid="$(printf '%s' "$r" | jq_r '.stories[-1].id')"
  olds="$(printf '%s' "$r" | jq_r '.stories[:-1][].id')"
  for i in $olds; do api POST "/api/autonomous/prds/$AID/remove_story" "{\"story_id\":\"$i\"}" >/dev/null; done
  r="$(api POST "/api/autonomous/prds/$AID/add_task" "$(jq -nc --arg s "$sid" --arg p "$spec" '{story_id:$s,title:"Live check task",spec:$p}')")"
  TID="$(printf '%s' "$r" | jq_r '[.stories[] | select(.id=="'"$sid"'")][0].tasks[-1].id')"
  # It stays in needs_review: that is a status in which a model or node pin is
  # accepted (an approved Automaton is locked against such overrides).
  [[ "$(prd_status "$AID")" == needs_review && -n "$TID" && "$TID" != null ]] || return 3
  return 0
}
set_task_model() { # set_task_model <model> — "<status> <body>"
  api_both POST "/api/autonomous/prds/$AID/set_task_llm" "$(jq -nc --arg t "$TID" --arg m "$1" '{task_id:$t,backend:"ollama",model:$m}')"
}
set_task_node() { # set_task_node <node> — "<status> <body>" (an empty node clears the pin)
  api_both POST "/api/autonomous/prds/$AID/set_task_node" "$(jq -nc --arg t "$TID" --arg n "$1" '{task_id:$t,node:$n}')"
}
mr_cfg() { api GET /api/config | jq -cS '.autonomous.model_routing' 2>/dev/null; }
mr_put() { # mr_put <json body> — "<status> <body>"
  api_both PUT /api/config "$1"
}
served() { # served [query] — GET /api/compute/served-models body
  api GET "/api/compute/served-models${1:+?$1}"
}
node_models() { # node_models <body> <node> — sorted models of one node, comma separated
  printf '%s' "$1" | jq -r --arg n "$2" '[.nodes[] | select(.node==$n)][0].models | sort | join(",")' 2>/dev/null
}
node_field() { # node_field <body> <node> <field>
  printf '%s' "$1" | jq -r --arg n "$2" ".nodes[] | select(.node==\$n) | .$3" 2>/dev/null
}

# --- MCP over stdio -------------------------------------------------------------
mcp_call() { # mcp_call <tool> <json arguments> -> the tool's result text; one retry on an empty answer
  local out i
  for i in 1 2 3; do
    out="$(mcp_call_once "$@")"
    [[ -n "${out//[$' \t\n']/}" ]] && break
    sleep 2
  done
  printf '%s\n' "$out"
}
mcp_call_once() { # one stdio session of `datawatch mcp`
  python3 -I - "$BIN" "$CFG" "$1" "$2" <<'PY'
import json, subprocess, sys, os, select, time
bin_, cfg, tool, args = sys.argv[1], sys.argv[2], sys.argv[3], json.loads(sys.argv[4])
env = {k: v for k, v in os.environ.items() if k not in ("TMUX", "TMUX_TMPDIR")}
p = subprocess.Popen([bin_, "--config", cfg, "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                     stderr=subprocess.DEVNULL, env=env, text=True)
def send(o): p.stdin.write(json.dumps(o) + "\n"); p.stdin.flush()
send({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2024-11-05", "capabilities": {}, "clientInfo": {"name": "model-routing-live-check", "version": "0"}}})
send({"jsonrpc": "2.0", "method": "notifications/initialized"})
send({"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": tool, "arguments": args}})
end = time.time() + 60
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

# ============================================================================
rep ""
rep "### Steps"

DEF_EXPECT='{"alert_on_refusal":true,"probe_timeout_seconds":8,"refusal_mode":"refuse","served_models_cache_seconds":300,"unknown_node_policy":"assume_serves","verifier_follows_worker_node":true}'

# ---- (a) config round trip -------------------------------------------------------
section "(a) Config round trip"
A0="$(mr_cfg)"
rep_block "\`GET /api/config\` → \`autonomous.model_routing\` before anything was set:" "$A0"
chk "the defaults read back: refusal_mode refuse, served_models_cache_seconds 300, verifier_follows_worker_node true, unknown_node_policy assume_serves, probe_timeout_seconds 8, alert_on_refusal true" "$(b01 "$A0" "$DEF_EXPECT")" "read: \`$A0\`"
SET_LINES=""
all_set_ok=1
for kv in 'refusal_mode|"warn"|warn' 'served_models_cache_seconds|17|17' 'verifier_follows_worker_node|false|false' 'unknown_node_policy|"assume_not"|assume_not' 'probe_timeout_seconds|3|3' 'alert_on_refusal|false|false'; do
  IFS='|' read -r key val want <<< "$kv"
  R="$(mr_put "{\"autonomous.model_routing.$key\":$val}")"
  got="$(api GET /api/config | jq -r ".autonomous.model_routing.$key" 2>/dev/null)"
  SET_LINES+="$key: PUT $(code_of "$R"), read back $got"$'\n'
  [[ "$(code_of "$R")" == 200 && "$got" == "$want" ]] || all_set_ok=0
done
rep_block "Each key set alone with \`PUT /api/config\`, then read back (HTTP status of the PUT, value read):" "$SET_LINES"
A1="$(mr_cfg)"
chk "each of the six keys was accepted (200) and reads back the value set" "$all_set_ok" "after all six: \`$A1\`"
# Back to the defaults, explicitly, before the refusal tests.
RESTORE='{"autonomous.model_routing.refusal_mode":"refuse","autonomous.model_routing.served_models_cache_seconds":300,"autonomous.model_routing.verifier_follows_worker_node":true,"autonomous.model_routing.unknown_node_policy":"assume_serves","autonomous.model_routing.probe_timeout_seconds":8,"autonomous.model_routing.alert_on_refusal":true}'
PR="$(mr_put "$RESTORE")"
chk "all six set back to the defaults in one request, and the block reads as at the start" "$([[ "$(code_of "$PR")" == 200 && "$(mr_cfg)" == "$DEF_EXPECT" ]] && echo 1 || echo 0)" "PUT $(code_of "$PR")"
BAD1="$(mr_put '{"autonomous.model_routing.refusal_mode":"sometimes","autonomous.model_routing.served_models_cache_seconds":99}')"
BAD2="$(mr_put '{"autonomous.model_routing.served_models_cache_seconds":-1,"autonomous.model_routing.alert_on_refusal":false}')"
BAD3="$(mr_put '{"autonomous.model_routing.probe_timeout_seconds":0,"autonomous.model_routing.refusal_mode":"warn"}')"
BAD4="$(mr_put '{"autonomous.model_routing.unknown_node_policy":"maybe","autonomous.model_routing.probe_timeout_seconds":20}')"
A2="$(mr_cfg)"
rep_block "Invalid values, each sent to \`PUT /api/config\` beside a valid key (HTTP status, then the refusal text):" "refusal_mode sometimes + cache 99: $BAD1
cache -1 + alert_on_refusal false: $BAD2
probe_timeout 0 + refusal_mode warn: $BAD3
unknown_node_policy maybe + probe_timeout 20: $BAD4"
chk "a bad mode, a negative cache age, a zero probe timeout and a bad policy are each refused with 400 and the refusal names the key" "$([[ "$(code_of "$BAD1")" == 400 && "$(code_of "$BAD2")" == 400 && "$(code_of "$BAD3")" == 400 && "$(code_of "$BAD4")" == 400 && "$BAD1" == *refusal_mode* && "$BAD2" == *served_models_cache_seconds* && "$BAD3" == *probe_timeout_seconds* && "$BAD4" == *unknown_node_policy* ]] && echo 1 || echo 0)" ""
chk "nothing was applied by a refused request, including the valid key sent beside the bad one (cache 99, alert_on_refusal false, refusal_mode warn, probe 20 not saved)" "$(b01 "$A2" "$DEF_EXPECT")" "config after the four refusals: \`$A2\`"
step_end a "Config round trip" "the six keys read back refuse, 300, true, assume_serves, 8, true; each was set and read back; four invalid requests were refused with 400 naming the key and the valid key beside each was not applied"

# ---- (b) served models on four surfaces -------------------------------------------
section "(b) Served models: REST, MCP, CLI and comm channel agree"
B_REST="$(served)"
B_MCP="$(mcp_call compute_served_models '{}')"
B_CLI_JSON="$(dw compute node served-models --json 2>"$S/cli.err")"
B_CLI_TXT="$(dw compute node served-models 2>>"$S/cli.err")"
B_COMM="$(api POST /api/test/message '{"text":"compute node models"}' | jq -r '.responses | join("\n")' 2>/dev/null)"
printf '%s' "$B_REST" > "$S/b-rest.json"; printf '%s' "$B_MCP" > "$S/b-mcp.json"
printf '%s' "$B_CLI_JSON" > "$S/b-cli.json"; printf '%s' "$B_CLI_TXT" > "$S/b-cli.txt"; printf '%s' "$B_COMM" > "$S/b-comm.txt"
B_VERDICT="$(python3 -I - "$S" "$NODE_A" "$NODE_B" "$MODEL_A,$MODEL_SHARED" "$MODEL_B,$MODEL_SHARED" <<'PY'
import json, sys
d, na, nb, ma, mb = sys.argv[1:6]
rd = lambda n: open(d + "/" + n).read()
problems = []
try:
    rest, mcp, cli = json.loads(rd("b-rest.json")), json.loads(rd("b-mcp.json")), json.loads(rd("b-cli.json"))
except Exception as e:
    print("a JSON read did not parse: %s" % e); sys.exit(1)
def view(doc, name):
    for n in doc.get("nodes", []):
        if n["node"] == name:
            return {"kind": n["kind"], "known": n["known"], "models": sorted(n["models"])}
    return None
want = {na: sorted(ma.split(",")), nb: sorted(mb.split(","))}
for name in (na, nb):
    r, m, c = view(rest, name), view(mcp, name), view(cli, name)
    if r is None: problems.append("REST has no node %s" % name); continue
    if r["models"] != want[name] or not r["known"]: problems.append("REST node %s reads %r" % (name, r))
    if m != r: problems.append("MCP differs from REST for %s" % name)
    if c != r: problems.append("CLI --json differs from REST for %s" % name)
    for label in ("b-cli.txt", "b-comm.txt"):
        text = rd(label)
        if name not in text: problems.append("%s misses node %s" % (label, name))
        for model in want[name]:
            if model not in text: problems.append("%s misses %s of %s" % (label, model, name))
print("OK" if not problems else "; ".join(problems))
sys.exit(0 if not problems else 1)
PY
)"
rep ""
rep "REST: \`GET /api/compute/served-models\`; MCP: \`compute_served_models {}\` over \`datawatch mcp\` (stdio); CLI: \`datawatch compute node served-models\` (text) and \`--json\`; comm: \`POST /api/test/message {\"text\":\"compute node models\"}\`."
rep_block "REST, the two sandbox nodes:" "$(printf '%s' "$B_REST" | jq --arg a "$NODE_A" --arg b "$NODE_B" '{cache_seconds,refusal_mode,nodes:[.nodes[] | select(.node==$a or .node==$b) | {node,kind,models,known,cached,age_seconds}]}')"
rep_block "CLI text:" "$B_CLI_TXT"
rep_block "Comm channel reply:" "$B_COMM"
rep_block "MCP result (first 500 bytes):" "$(printf '%s' "$B_MCP" | head -c 500)"
chk "REST reads node A as $MODEL_A and $MODEL_SHARED, and node B as $MODEL_B and $MODEL_SHARED, both known" "$([[ "$(node_models "$B_REST" "$NODE_A")" == "$MODEL_A,$MODEL_SHARED" && "$(node_models "$B_REST" "$NODE_B")" == "$MODEL_B,$MODEL_SHARED" && "$(node_field "$B_REST" "$NODE_A" known)" == true && "$(node_field "$B_REST" "$NODE_B" known)" == true ]] && echo 1 || echo 0)" "A: $(node_models "$B_REST" "$NODE_A"); B: $(node_models "$B_REST" "$NODE_B")"
chk "MCP and CLI \`--json\` return the same kind, known flag and models for both nodes as REST, and the CLI text and the comm reply name both nodes with every model" "$(b01 "$B_VERDICT" OK)" "comparison: \`$B_VERDICT\`; CLI stderr: $(head -c 200 "$S/cli.err")"
step_end b "Served models read on REST, MCP, CLI and comm" "both nodes read the same models on REST, MCP and CLI --json, and the CLI text and comm reply carried both node names with every model"

# ---- Automaton for the pin steps ---------------------------------------------------
AUT_OK=0
if new_automaton "model-routing" "Placeholder task for the model routing live check."; then
  AUT_OK=1
  rep ""
  rep "Throwaway Automaton \`$AID\`, one story, one task \`$TID\`, created and planned through the API and left in \`needs_review\`, the status in which a model or node pin is accepted. It is never approved or run."
fi

# ---- (c) pin-time refusal ---------------------------------------------------------
section "(c) Pin-time refusal"
if [[ "$AUT_OK" == 1 ]]; then
  C_ALERTS0="$(alerts_count)"
  C_REF="$(set_task_model "$PIN_NONE")"
  C_MODEL_AFTER="$(tfield "$AID" "$TID" '(.model // "")')"
  C_ALERT_LIST="$(alerts_named | jq -c --arg m "$PIN_NONE" '[.[] | select((.body // "") | contains($m) and contains("was refused"))]')"
  C_OK="$(set_task_model "$PIN_A")"
  C_MODEL_OK="$(tfield "$AID" "$TID" '(.model // "")')"
  rep_block "\`POST /api/autonomous/prds/$AID/set_task_llm\` with model \`$PIN_NONE\` (served by neither node), HTTP status then body:" "$C_REF"
  rep_block "The same call with model \`$PIN_A\` (served by node A only):" "$C_OK"
  chk "the unserved model is refused with 400" "$(b01 "$(code_of "$C_REF")" 400)" "answer: $(code_of "$C_REF")"
  chk "the refusal names the model and both nodes" "$([[ "$C_REF" == *"$PIN_NONE"* && "$C_REF" == *"$NODE_A"* && "$C_REF" == *"$NODE_B"* ]] && echo 1 || echo 0)" "text: \`$(body_of "$C_REF" | head -c 400)\`"
  chk "nothing was stored by the refusal: the task's model is still empty" "$(b01 "$C_MODEL_AFTER" "")" "task model after the refusal: \`$C_MODEL_AFTER\`"
  chk "the refusal raised one error-level alert titled \`$ALERT_TITLE\` (alert_on_refusal is on by default)" "$([[ "$(alerts_count)" -gt "$C_ALERTS0" && "$(printf '%s' "$C_ALERT_LIST" | jq -r '.[0].level // .[0].severity // ""')" == error ]] && echo 1 || echo 0)" "alerts with that title: before $C_ALERTS0, after $(alerts_count); the refusal alert is at level \`$(printf '%s' "$C_ALERT_LIST" | jq -r '.[0].level // .[0].severity // "?"')\`"
  chk "a model one node serves is accepted (200) and stored on the task" "$([[ "$(code_of "$C_OK")" == 200 && "$C_MODEL_OK" == "$PIN_A" ]] && echo 1 || echo 0)" "answer $(code_of "$C_OK"); task model now \`$C_MODEL_OK\`"
else
  chk "the throwaway Automaton was created and planned" 0 "new_automaton failed: ${NEW_DIAG:-no detail} (status $(prd_status "${AID:-x}"))"
fi
step_end c "Pin-time refusal" "a model neither node serves was refused with 400 naming the model and both nodes, nothing was stored and an error alert was raised; a model node A serves was accepted and stored"

# ---- (d) warn mode ------------------------------------------------------------------
section "(d) Warn mode"
if [[ "$AUT_OK" == 1 ]]; then
  mr_put '{"autonomous.model_routing.refusal_mode":"warn"}' >/dev/null
  D_ALERTS0="$(alerts_count)"
  D_DEC0="$(dec_count "$AID" model_not_served_warning)"
  D_W="$(set_task_model "$PIN_NONE")"
  D_MODEL="$(tfield "$AID" "$TID" '(.model // "")')"
  D_DEC1="$(dec_count "$AID" model_not_served_warning)"
  D_NOTE="$(prd_get "$AID" | jq -r '[.decisions[]? | select(.kind=="model_not_served_warning")][-1].note // ""')"
  D_ALERT_LIST="$(alerts_named | jq -c --arg m "$PIN_NONE" '[.[] | select((.body // "") | contains($m) and contains("set to warn"))]')"
  D_ALERTS1="$(alerts_count)"
  rep_block "With \`refusal_mode\` warn, \`set_task_llm\` with model \`$PIN_NONE\`, HTTP status then body:" "$D_W"
  rep_block "The newest \`model_not_served_warning\` decision note, and the alert the warning raised:" "$D_NOTE
$(printf '%s' "$D_ALERT_LIST" | jq -c '.[0] | {title,level,body}')"
  chk "the unserved model is applied: the answer is 200 and the task's model is now \`$PIN_NONE\`" "$([[ "$(code_of "$D_W")" == 200 && "$D_MODEL" == "$PIN_NONE" ]] && echo 1 || echo 0)" "answer $(code_of "$D_W"); task model \`$D_MODEL\`"
  chk "a \`model_not_served_warning\` decision was recorded on the Automaton and names the model and both nodes" "$([[ "$D_DEC1" -gt "$D_DEC0" && "$D_NOTE" == *"$PIN_NONE"* && "$D_NOTE" == *"$NODE_A"* && "$D_NOTE" == *"$NODE_B"* ]] && echo 1 || echo 0)" "decisions of that kind: before $D_DEC0, after $D_DEC1"
  chk "an alert exists on the alerts API, at warning level (the alerts API spells it \`warn\`)" "$([[ "$D_ALERTS1" -gt "$D_ALERTS0" && "$(printf '%s' "$D_ALERT_LIST" | jq -r '.[0].level // .[0].severity // ""')" =~ ^warn(ing)?$ ]] && echo 1 || echo 0)" "alerts with that title: before $D_ALERTS0, after $D_ALERTS1; the warn-mode alert is at level \`$(printf '%s' "$D_ALERT_LIST" | jq -r '.[0].level // .[0].severity // "?"')\`"
  mr_put '{"autonomous.model_routing.alert_on_refusal":false}' >/dev/null
  D_ALERTS2="$(alerts_count)"
  D_DEC2="$(dec_count "$AID" model_not_served_warning)"
  D_W2="$(set_task_model "$PIN_NONE2")"
  D_ALERTS3="$(alerts_count)"
  D_DEC3="$(dec_count "$AID" model_not_served_warning)"
  rep_block "With \`alert_on_refusal\` false, \`set_task_llm\` with model \`$PIN_NONE2\`, HTTP status then body:" "$D_W2"
  chk "with alert_on_refusal false the pin is still applied and recorded (200, a second warning decision)" "$([[ "$(code_of "$D_W2")" == 200 && "$D_DEC3" -gt "$D_DEC2" && "$(tfield "$AID" "$TID" '(.model // "")')" == "$PIN_NONE2" ]] && echo 1 || echo 0)" "decisions: $D_DEC2 -> $D_DEC3"
  chk "and no alert is raised: the count of alerts with that title did not change" "$(b01 "$D_ALERTS3" "$D_ALERTS2")" "alerts with that title: $D_ALERTS2 -> $D_ALERTS3"
  mr_put '{"autonomous.model_routing.alert_on_refusal":true,"autonomous.model_routing.refusal_mode":"refuse"}' >/dev/null
  set_task_model "$PIN_A" >/dev/null
  chk "refuse mode and alert_on_refusal are back at their defaults and the task's model is back on \`$PIN_A\`" "$([[ "$(mr_cfg)" == "$DEF_EXPECT" && "$(tfield "$AID" "$TID" '(.model // "")')" == "$PIN_A" ]] && echo 1 || echo 0)" ""
else
  chk "the throwaway Automaton exists" 0 ""
fi
step_end d "Warn mode" "with refusal_mode warn the unserved model was applied, a model_not_served_warning decision naming the model and both nodes was recorded and a warning alert existed on the alerts API; with alert_on_refusal false the pin was applied and recorded and no alert was raised"

# ---- (e) node pin -------------------------------------------------------------------
section "(e) Node pin"
if [[ "$AUT_OK" == 1 ]]; then
  E_BAD="$(set_task_node "$NODE_B")"
  E_PIN_AFTER_BAD="$(tfield "$AID" "$TID" '(.pinned_node // "")')"
  E_OK="$(set_task_node "$NODE_A")"
  E_PIN_OK="$(tfield "$AID" "$TID" '(.pinned_node // "")')"
  E_JSON="$(prd_get "$AID" | jq -c --arg t "$TID" '[.stories[].tasks[] | select(.id==$t)][0] | {id,model,pinned_node}')"
  E_CLEAR="$(set_task_node "")"
  E_PIN_CLEARED="$(prd_get "$AID" | jq -c --arg t "$TID" '[.stories[].tasks[] | select(.id==$t)][0] | has("pinned_node")')"
  E_UNREG="$(set_task_node "live-node-unregistered")"
  rep_block "Pin the task (model \`$PIN_A\`, served by node A only) to node B, HTTP status then body:" "$E_BAD"
  rep_block "Pin it to node A, then the task as the Automaton JSON shows it:" "$E_OK
$E_JSON"
  rep_block "Clear the pin (empty node), then pin to a node name that is not registered:" "clear: $E_CLEAR
unregistered: $E_UNREG"
  chk "pinning to the node that does not serve the model is refused (400) and names the node and the model" "$([[ "$(code_of "$E_BAD")" == 400 && "$E_BAD" == *"$NODE_B"* && "$E_BAD" == *"$PIN_A"* ]] && echo 1 || echo 0)" "text: \`$(body_of "$E_BAD" | head -c 300)\`"
  chk "the refused pin stored nothing (no pinned_node on the task)" "$(b01 "$E_PIN_AFTER_BAD" "")" ""
  chk "pinning to the serving node is accepted (200) and the Automaton JSON shows pinned_node \`$NODE_A\` on the task" "$([[ "$(code_of "$E_OK")" == 200 && "$E_PIN_OK" == "$NODE_A" ]] && echo 1 || echo 0)" "task JSON: \`$E_JSON\`"
  chk "clearing the pin (empty node) is accepted and pinned_node is gone from the task" "$([[ "$(code_of "$E_CLEAR")" == 200 && "$E_PIN_CLEARED" == false ]] && echo 1 || echo 0)" "clear answer $(code_of "$E_CLEAR"); pinned_node key present: $E_PIN_CLEARED"
  chk "a node name that is not registered is refused (400) and the answer names it" "$([[ "$(code_of "$E_UNREG")" == 400 && "$E_UNREG" == *live-node-unregistered* ]] && echo 1 || echo 0)" "text: \`$(body_of "$E_UNREG" | head -c 300)\`"
else
  chk "the throwaway Automaton exists" 0 ""
fi
step_end e "Node pin" "a pin to the node not serving the model was refused naming node and model, a pin to the serving node was accepted and showed as pinned_node, clearing removed it, and an unregistered node was refused"

# ---- (f) cache age ------------------------------------------------------------------
section "(f) Cache age"
F_NEW1="live-late-one:1b"
F_NEW2="live-late-two:1b"
F_BASE="$(served "node=$NODE_A")"
set_list A "$MODEL_A" "$MODEL_SHARED" "$F_NEW1"
F_PLAIN="$(served "node=$NODE_A")"
F_REFRESH="$(served "node=$NODE_A&refresh=true")"
F_AFTER="$(served "node=$NODE_A")"
rep ""
rep "The stand-in for node A was changed to also serve \`$F_NEW1\` while the cache age was 300 seconds."
rep_block "Plain read of node A inside the age, then with \`refresh=true\`, then plain again:" "plain:   $(printf '%s' "$F_PLAIN" | jq -c '.nodes[0] | {models,cached,age_seconds}')
refresh: $(printf '%s' "$F_REFRESH" | jq -c '.nodes[0] | {models,cached,age_seconds}')
after:   $(printf '%s' "$F_AFTER" | jq -c '.nodes[0] | {models,cached,age_seconds}')"
chk "inside the age a plain read still shows the old list (no \`$F_NEW1\`) and says it is cached" "$([[ "$(node_models "$F_PLAIN" "$NODE_A")" == "$MODEL_A,$MODEL_SHARED" && "$(node_field "$F_PLAIN" "$NODE_A" cached)" == true ]] && echo 1 || echo 0)" "models: $(node_models "$F_PLAIN" "$NODE_A"); cached: $(node_field "$F_PLAIN" "$NODE_A" cached)"
chk "refresh=true shows the new list (with \`$F_NEW1\`), not cached" "$([[ "$(node_models "$F_REFRESH" "$NODE_A")" == *"$F_NEW1"* && "$(node_field "$F_REFRESH" "$NODE_A" cached)" == false ]] && echo 1 || echo 0)" "models: $(node_models "$F_REFRESH" "$NODE_A")"
chk "the refresh replaced the kept copy: the next plain read shows the new list" "$([[ "$(node_models "$F_AFTER" "$NODE_A")" == *"$F_NEW1"* ]] && echo 1 || echo 0)" ""
F_PUT="$(mr_put '{"autonomous.model_routing.served_models_cache_seconds":0}')"
set_list A "$MODEL_A" "$MODEL_SHARED" "$F_NEW2"
F_ZERO="$(served "node=$NODE_A")"
rep_block "With \`served_models_cache_seconds\` set to 0 (PUT $(code_of "$F_PUT")) and the stand-in changed to \`$F_NEW2\`, one plain read:" "$(printf '%s' "$F_ZERO" | jq -c '{cache_seconds,node:.nodes[0]|{models,cached,age_seconds}}')"
chk "with the age at 0 the next plain read is fresh: it shows \`$F_NEW2\`, not cached, and the answer reports cache_seconds 0" "$([[ "$(node_models "$F_ZERO" "$NODE_A")" == *"$F_NEW2"* && "$(node_field "$F_ZERO" "$NODE_A" cached)" == false && "$(printf '%s' "$F_ZERO" | jq -r '.cache_seconds')" == 0 ]] && echo 1 || echo 0)" "models: $(node_models "$F_ZERO" "$NODE_A")"
mr_put '{"autonomous.model_routing.served_models_cache_seconds":300}' >/dev/null
set_list A "$MODEL_A" "$MODEL_SHARED"
served "refresh=true" >/dev/null
chk "the cache age is back at 300 and node A serves its original list again" "$([[ "$(mr_cfg)" == "$DEF_EXPECT" && "$(node_models "$(served)" "$NODE_A")" == "$MODEL_A,$MODEL_SHARED" ]] && echo 1 || echo 0)" ""
step_end f "Cache age" "inside the age a plain read kept the old list, refresh=true showed the new one, and with the age at 0 the next plain read was fresh"

# ---- (g) the verifier follows the worker's node -----------------------------------------
section "(g) The verifier follows the worker's node"
rep ""
rep "Not exercised live. Placing a verification job on the node its worker ran on needs a running worker, and the sandbox has no model to run one. This behavior is covered by the Go test \`TestModelRoute_VerifierFollowsPinnedNode\` (and by the \`TestVerifierNode_\` tests), which use fakes; this run did not drive it."
RESULTS+=("g|NOT EXERCISED|The verifier follows the worker's node|not exercised live: it needs a running worker, which the sandbox cannot drive; covered by the Go test \`TestModelRoute_VerifierFollowsPinnedNode\`")
say "  [g] NOT EXERCISED — covered by TestModelRoute_VerifierFollowsPinnedNode"

# ============================================================================
say ""; say "== Cleanup =="; rep ""; rep "### Cleanup"
for id in "${CREATED[@]:-}"; do [[ -n "$id" ]] && drop_automaton "$id"; done
drop_sessions
drop_registrations
REMAIN_PRDS="$(api GET /api/autonomous/prds | jq_r '(.prds // .automata // []) | length')"
REMAIN_NODES="$(api GET /api/compute/nodes | jq_r '[(if type=="array" then . else .nodes end)[]? | select(.name==("'"$NODE_A"'","'"$NODE_B"'"))] | length')"
REMAIN_LLM="$(api GET /api/llms | jq_r '[.llms[]? | select(.name=="'"$LLM_NAME"'")] | length')"
rep ""
rep "- Created through the API and removed through the API: ${#CREATED[@]} Automaton (cancelled, hard-deleted), the LLM entry \`$LLM_NAME\` and the two compute nodes. After the deletes \`GET /api/autonomous/prds\` listed ${REMAIN_PRDS:-?} Automata, \`GET /api/compute/nodes\` listed ${REMAIN_NODES:-?} of this check's nodes and \`GET /api/llms\` listed ${REMAIN_LLM:-?} of this check's entries."
td_stop_all
sleep 1
D_GONE="no"; td_alive "$DPID" || D_GONE="yes"
P_GONE="no"; td_alive "$PLAN_PID" || P_GONE="yes"
A_GONE="no"; td_alive "$SA_PID" || A_GONE="yes"
B_GONE="no"; td_alive "$SB_PID" || B_GONE="yes"
PORT_CLOSED="no"; td_wait_port_closed "$TLS_PORT" 20 && PORT_CLOSED="yes"
SA_CLOSED="no"; td_wait_port_closed "$SA_PORT" 20 && SA_CLOSED="yes"
LEFT_OUT="$(env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" check "$LEFT_SNAP" 2>&1)"; LEFT_RC=$?
# A daemon that is new since the snapshot but whose config is not under this
# check's scratch area belongs to another job on the host, not to this check.
LEFT_OURS="$(printf '%s\n' "$LEFT_OUT" | awk -v s="config=$S/" 'index($0, s) > 0' | wc -l)"
LEFT_OTHERS=$(( $(printf '%s\n' "$LEFT_OUT" | grep -c 'leftover test daemon:') - LEFT_OURS ))
rep "- **Sandbox daemon ended** by \`td_stop_all\` (recorded pid \`$DPID\`, marker = its config path); planner stand-in and both model stand-ins ended the same way (pids \`$PLAN_PID\`, \`$SA_PID\`, \`$SB_PID\`, marker = their script or list file). Daemon gone: $D_GONE. Planner gone: $P_GONE. Stand-in A gone: $A_GONE. Stand-in B gone: $B_GONE. TLS port $TLS_PORT closed: $PORT_CLOSED. Stand-in port $SA_PORT closed: $SA_CLOSED."
rep "- **Leftover check** against the snapshot taken before this run: \`scripts/check-no-leftover-test-daemons.sh check\` exit $LEFT_RC — $(printf '%s' "$LEFT_OUT" | tail -1). New daemons whose config lies in this check's scratch area: $LEFT_OURS. New daemons of other jobs on the host: $LEFT_OTHERS."
rep "- **Scratch area** (\`.datawatch/live-check/\`: project, config, data, log, list files, shims, daemon binary) is removed by the exit handler right after this report is written."
STEP_OK=1
CLEAN_OK="$([[ "$D_GONE" == yes && "$P_GONE" == yes && "$A_GONE" == yes && "$B_GONE" == yes && "$PORT_CLOSED" == yes && "$LEFT_OURS" == 0 && "${REMAIN_PRDS:-1}" == 0 && "${REMAIN_NODES:-1}" == 0 && "${REMAIN_LLM:-1}" == 0 ]] && echo 1 || echo 0)"
chk "the sandbox daemon and both stand-ins are gone, every created Automaton, LLM entry and node was removed, and no test daemon was left behind" "$CLEAN_OK" "daemon gone: $D_GONE; planner gone: $P_GONE; stand-ins gone: $A_GONE/$B_GONE; port closed: $PORT_CLOSED; leftover check exit $LEFT_RC (new daemons from this check: $LEFT_OURS, from other jobs: $LEFT_OTHERS); Automata left: ${REMAIN_PRDS:-?}; nodes left: ${REMAIN_NODES:-?}; LLM entries left: ${REMAIN_LLM:-?}"
step_end cleanup "Cleanup" "the daemon and all three stand-ins were gone, no Automaton, node or LLM entry was left, and the leftover guard found no test daemon"
if [[ "$CLEAN_OK" == 1 ]]; then
  CLEANUP_TXT="daemon and stand-ins ended by recorded pid, TLS port $TLS_PORT closed, created Automaton, LLM entry and nodes deleted through the API, no leftover test daemon from this check, scratch area removed."
else
  CLEANUP_TXT="NOT CLEAN — see the Cleanup section of the evidence."
fi

# ---- what is not shown live ----------------------------------------------------------------
rep ""
rep "## What is not shown live"
rep ""
rep "- **The verifier following the worker's node (g)** needs a running worker and is not drivable in the sandbox without one. It is covered by the Go test \`TestModelRoute_VerifierFollowsPinnedNode\` and was not exercised live."
rep "- **The nodes are stand-ins.** They answer \`GET /api/tags\` only; no model is loaded or run. A real engine's model list (tags, digests) and a real node that is down were not exercised; the unreachable-node policy (\`unknown_node_policy\`) and \`probe_timeout_seconds\` were set and read back in step (a) but no node was made unreachable here."
rep "- **Only the task model setter and the task node pin were driven.** The story and Automaton level setters, the verifier model setters and the story node pin share the same check and are covered by Go tests, not driven here."
rep "- **Web app and mobile app** were not driven; their tracker rows are not changed by this check."
rep "- **The comm read** went through \`POST /api/test/message\` on the sandbox daemon with \`server.token\` set, so it is the router's own verb that ran. A real chat channel was not connected."

OVERALL=PASS
[[ "$FAIL" -gt 0 ]] && OVERALL=FAIL
write_report "$OVERALL"
say ""
say "Summary: $PASS passed, $FAIL failed, 1 not exercised live — report: $REPORT"
[[ "$FAIL" -eq 0 ]]
