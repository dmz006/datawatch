#!/usr/bin/env bash
# request-watch-live-check.sh — live check of the Automaton model-request watch
# on a SANDBOX daemon, against a fake compute node.
#
# What the feature does: the daemon observes the in-flight model request of a
# worker (or verifier) session whose model runs on a registered compute node,
# so a hung request can be told from a slow one. It is observation only: no
# model call, no remediation.
#
# What it proves (each result is a numbered check in the report):
#   1. Config: all eleven autonomous.request_watch keys round-trip through PUT
#      then GET /api/config and through the Automata settings route, the
#      defaults read on a fresh config, and a bad value is a 400 that names
#      the key.
#   2. Generating: with the fake node generating, GET /api/autonomous/request-watch,
#      the MCP tool autonomous_request_watch, the CLI (--json and text) and the
#      chat verb all report generating and agree; the session JSON and the
#      task view carry model_request.
#   3. Idle and threshold: with the node idle the no-progress time grows, the
#      summary reads "no progress for N minutes", and once past the threshold
#      exactly one alert and one question appear (four options, keep_waiting
#      default, not blocking); a later poll adds none.
#   4. Answer records only: answering the question with end_worker records one
#      decision; the session is still running, the task status is unchanged,
#      and the fake node saw only read requests of its running-models listing.
#   5. Recovery: with the node generating again the state returns to
#      generating, and an open question is withdrawn.
#   6. Unreachable: with the fake node stopped the state is unreachable; with
#      unreachable_counts true it crosses the threshold, with false it does not.
#   7. Disabled and role flags: enabled=false leaves the read empty,
#      observe_workers=false hides the worker.
#   8. Cleanup: the daemon, the fake node and every object this script created
#      are gone and no test daemon is left behind.
#
# How the worker is placed on the fake node. The fake node is registered as a
# compute node of kind ollama through the REST API, and an LLM entry that lists
# it as its only node is registered too. The Automaton names that entry as its
# backend, so the daemon's own executor starts the task session on the node,
# exactly as it does for a real node. The worker itself is the daemon's shell
# backend running a generated script that waits (a request that is in flight
# and never ends); no model is involved anywhere.
#
# The Automaton is written into the sandbox's own store before the daemon's
# first start (the create route only makes drafts and a plan needs a planner
# model). From the moment the daemon is up everything goes through its REST
# API: approve, run, answers, reads, settings, and the deletes at the end.
#
# The fake node is a small python3 HTTP server written into the throwaway
# folder. It serves GET /api/ps only: in the generating mode the expiry time of
# the loaded model changes on every read, in the idle mode the body is the same
# every time. Every request (method and path) is logged to a file. It is ended
# by the PID this script recorded, like the daemon.
#
# Safety: SANDBOX only. The daemon runs from a generated config under one
# throwaway folder on loopback ports probed just before use, with its own data
# directory, its own TMPDIR and its own multiplexer socket directory, so no
# session of the operator is reachable. The production daemon, its service and
# its ports are never touched. The daemon and the fake node are ended only by
# the PIDs scripts/test-daemon-lib.sh recorded (marker = the config or script
# path). Everything this script made is removed on every exit path.
#
# Usage (always from a shell with the operator's multiplexer variables removed):
#   env -u TMUX -u TMUX_TMPDIR bash scripts/request-watch-live-check.sh
#
# Env:
#   REQUEST_WATCH_CHECK_REPORT  where the tracked report is written
#                               (default: docs/plans/2026-10-31-model-request-progress-live-check.md)
#   TEST_BINARY                 daemon binary (default: bin/datawatch, built from
#                               this tree when missing, then PATH)
#
# Exit 0 only when every check passed.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/scripts/test-daemon-lib.sh"
REPORT="${REQUEST_WATCH_CHECK_REPORT:-$ROOT/docs/plans/2026-10-31-model-request-progress-live-check.md}"

unset TMUX TMUX_TMPDIR

# --- names this check uses (all of them are made up here, none is configuration
# the daemon reads from the repository) ---------------------------------------
NODE_NAME="livecheck-node"
LLM_NAME="livecheck-llm"
MODEL_NAME="livecheck-model:1b"
AUTOMATON_ID="c0de0001"
TASK_ID="${AUTOMATON_ID}t1"
ALERT_TITLE="Automaton model request is not progressing"
POLL_SECONDS=2
NO_PROGRESS_MINUTES=1

# --- scratch area and lifecycle ----------------------------------------------
LC="$ROOT/.datawatch/live-check"
mkdir -p "$LC"
S="$(mktemp -d "$LC/rw.XXXXXX")"   # short on purpose: a unix socket path is limited to about 100 characters
export TD_STATE_DIR="$S/tdstate"
# shellcheck source=/dev/null
source "$LIB"

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
  if [[ "$v" == FAIL && -n "$ev" ]]; then say "        evidence: $ev"; fi
  rep ""
  rep "- **$v** — $what"
  if [[ -n "$ev" ]]; then rep "  - $ev"; fi
}
now() { date '+%H:%M:%S'; }

# --- prerequisites ---------------------------------------------------------
for t in git curl jq; do
  command -v "$t" >/dev/null 2>&1 || { say "SKIP: $t is not on PATH"; rm -rf "$S"; exit 0; }
done
PY="$(command -v python3 2>/dev/null || true)"
[[ -n "$PY" ]] || { say "SKIP: python3 is not on PATH"; rm -rf "$S"; exit 0; }

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
[[ -n "$BINARY" ]] || { say "SKIP: no datawatch binary (TEST_BINARY, bin/datawatch, PATH)"; rm -rf "$S"; exit 0; }
VERSION="$("$BINARY" version 2>/dev/null | head -1 || true)"

free_port() {
  "$PY" -I -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}
PORT="$(free_port)"; TLS_PORT="$(free_port)"; MCP_PORT="$(free_port)"
DNS_PORT="$(free_port)"; HOOK_PORT="$(free_port)"; NTFY_PORT="$(free_port)"
NODE_PORT="$(free_port)"; DEAD_PORT="$(free_port)"
TOKEN="dw-test-token-12345"
BASE="https://127.0.0.1:$TLS_PORT"

DATA="$S/data"
PROJ="$S/projects"          # allowed project root
WORKDIR="$PROJ/work"        # the Automaton's project directory
DTMP="$S/daemon-tmp"        # the daemon's own TMPDIR
SOCKDIR="$S/r"              # the daemon's multiplexer socket directory (kept short)
STATE="$S/worker-state"     # what the stand-in worker remembers
CFG="$S/config.yaml"
LOG="$S/daemon.log"
NODE_PY="$S/fake-node.py"
NODE_MODE="$S/node.mode"    # control file: generating or idle
NODE_LOG="$S/node-requests.log"
NODE_ERR="$S/node.err"
mkdir -p "$DATA" "$PROJ" "$WORKDIR" "$DTMP" "$SOCKDIR" "$STATE" "$S/bin"
# The multiplexer socket lives at <dir>/tmux-<uid>/default: refuse a path that cannot fit.
if (( ${#SOCKDIR} + 24 > 104 )); then say "FAIL: the socket directory path is too long for a unix socket: $SOCKDIR"; exit 1; fi

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
    sleep 1
  done
  return 1
}

# --- cleanup on every exit path ----------------------------------------------
FAKE_PID=""
cleanup_pre() {
  # Runs BEFORE the daemon is ended, while its API still answers: remove what
  # this script created through the API. Every step is safe to repeat.
  if [[ -n "${DPID:-}" ]] && td_alive "$DPID"; then
    api DELETE "/api/autonomous/prds/$AUTOMATON_ID" >/dev/null 2>&1 || true
    sleep 2
    api DELETE "/api/autonomous/prds/$AUTOMATON_ID?hard=true" >/dev/null 2>&1 || true
    local sid
    for sid in $(api GET /api/sessions 2>/dev/null | jq -r '.[]? | select((.name // "") | startswith("autonomous:")) | .full_id // .id' 2>/dev/null); do
      api POST /api/sessions/delete "{\"id\":\"$sid\"}" >/dev/null 2>&1 || true
    done
    api DELETE "/api/llms/$LLM_NAME" >/dev/null 2>&1 || true
    api DELETE "/api/compute/nodes/$NODE_NAME" >/dev/null 2>&1 || true
  fi
  : > "$STATE/stop"
}
cleanup_post() {
  # Runs AFTER the daemon and the fake node are ended: remove the scratch area.
  rm -rf "$S" 2>/dev/null || true
}
TD_PRE_STOP_CLEANUP=cleanup_pre
TD_EXTRA_CLEANUP=cleanup_post
td_install_traps "$@"

# --- the fake compute node -----------------------------------------------------
write_fake_node() {
  cat > "$NODE_PY" <<'PYNODE'
import http.server, json, sys, threading

port, mode_file, log_file, model = int(sys.argv[1]), sys.argv[2], sys.argv[3], sys.argv[4]
lock = threading.Lock()
reads = [0]

def mode():
    try:
        with open(mode_file) as f:
            return f.read().strip() or "generating"
    except OSError:
        return "generating"

class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.0"

    def log_message(self, fmt, *args):
        pass

    def _log(self):
        with lock:
            with open(log_file, "a") as f:
                f.write("%s %s\n" % (self.command, self.path))

    def _answer(self):
        self._log()
        if self.command == "GET" and self.path.split("?")[0] == "/api/ps":
            with lock:
                if mode() == "generating":
                    reads[0] += 1
                n = reads[0]
            body = {"models": [{
                "name": model, "model": model,
                "expires_at": "2030-01-01T00:%02d:%02d+00:00" % ((n // 60) % 60, n % 60),
                "size_vram": 1000000, "context_length": 4096,
            }]}
            data = json.dumps(body).encode()
            self.send_response(200)
        else:
            data = b'{"error":"not found"}'
            self.send_response(404)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    do_GET = do_POST = do_PUT = do_DELETE = do_HEAD = _answer

srv = http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler)
srv.daemon_threads = True
srv.serve_forever()
PYNODE
}

node_mode() { printf '%s\n' "$1" > "$NODE_MODE"; }
node_log_lines() { wc -l < "$NODE_LOG" 2>/dev/null | tr -d ' ' || echo 0; }

start_fake_node() {
  node_mode generating
  : > "$NODE_LOG"
  "$PY" -I "$NODE_PY" "$NODE_PORT" "$NODE_MODE" "$NODE_LOG" "$MODEL_NAME" > /dev/null 2> "$NODE_ERR" &
  FAKE_PID=$!
  td_track_pid "$FAKE_PID" "$NODE_PY"
  local end=$((SECONDS + 15))
  while ((SECONDS < end)); do
    if (exec 3<>"/dev/tcp/127.0.0.1/$NODE_PORT") 2>/dev/null; then return 0; fi
    sleep 0.3
  done
  return 1
}

# --- the stand-in worker and the store -----------------------------------------
write_worker() {
  # The shell backend runs this with the task text as $1 and the project
  # directory as $2. It stands for a request that is in flight and never ends.
  cat > "$S/bin/worker.sh" <<WORKER
#!/bin/sh
printf 'worker started\n' >> "$STATE/runs.log"
while [ ! -e "$STATE/stop" ]; do printf 'waiting on the model request\\n'; sleep 2; done
exit 0
WORKER
  chmod +x "$S/bin/worker.sh"
}

seed_store() {
  # Before the first start: one Automaton awaiting review with one story and one
  # task, naming the LLM entry (registered later through the API) as backend.
  mkdir -p "$DATA/autonomous"
  "$PY" -I - "$DATA/autonomous/prds.jsonl" "$AUTOMATON_ID" "$LLM_NAME" "$WORKDIR" "$MODEL_NAME" <<'PY'
import json, sys, datetime
out, aid, llm, workdir, model = sys.argv[1:6]
now = datetime.datetime.now(datetime.timezone.utc).isoformat()
task = {"id": aid + "t1", "story_id": aid + "s1", "prd_id": aid,
        "title": "Wait on a model request", "spec": "Wait on a model request that never ends.",
        "status": "pending", "created_at": now, "updated_at": now}
rec = {"id": aid, "title": "request watch live check", "spec": "Live check of the model request watch.",
       "project_dir": workdir, "status": "needs_review", "backend": llm, "model": model,
       "stories": [{"id": aid + "s1", "prd_id": aid, "title": "Sample story", "status": "pending",
                    "tasks": [task], "created_at": now, "updated_at": now}],
       "created_at": now, "updated_at": now}
with open(out, "w") as f:
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
    -e "s|http://local-node:11434|http://127.0.0.1:$DEAD_PORT|g" \
    -e "s|http://localhost:11434|http://127.0.0.1:$DEAD_PORT|g" \
    "$ROOT/testdata/datawatch.yaml" > "$CFG"
  # The operator question routes (off by default), the allowed project root and
  # the stand-in worker, through the normal configuration keys.
  sed -i \
    -e "s|^autonomous:\$|autonomous:\n  questions:\n    enabled: true|" \
    -e "s|^server:\$|server:\n  allowed_project_roots: [\"$PROJ\"]|" \
    -e "s|^shell_backend:\$|shell_backend:\n  script_path: $S/bin/worker.sh|" \
    "$CFG"
}

# --- reads ---------------------------------------------------------------
cfg_get() { api GET /api/config | jq_r "$1"; }
rw() { api GET /api/autonomous/request-watch; }
obs() { rw | jq -c '[.observations[]?][0] // {}'; }                      # the one observation
obs_f() { obs | jq_r "$1"; }                                              # one field of it
alerts_n() { api GET /api/alerts | jq_r --arg t "$ALERT_TITLE" '[.alerts[]? | select(.title==$t)] | length'; }
open_q() { api GET "/api/questions?state=open&asker=request-watch&limit=100" | jq -c '[.questions[]? // empty]' 2>/dev/null; }
open_q_n() { open_q | jq_r 'length'; }
prd() { api GET "/api/autonomous/prds/$AUTOMATON_ID"; }
task_status() { prd | jq_r --arg t "$TASK_ID" '[.stories[]?.tasks[]? | select(.id==$t)][0].status'; }
sess_state() { api GET "/api/sessions/$SID" | jq_r '.state'; }
# The executor marks a task verifying as soon as its one-shot worker is started and
# keeps it there while the worker runs, so "in flight" is in_progress or verifying.
task_flight() { case "$(task_status)" in in_progress|verifying) echo yes;; *) echo no;; esac; }
dlog() { cat "$LOG" 2>/dev/null; }
put_cfg() { api_code PUT /api/config "$1"; }

# Conditions for wait_until.
c_obs_state() { [[ "$(obs_f '.state')" == "$1" ]]; }
c_over() { [[ "$(obs_f '.over_threshold')" == true ]]; }
c_not_over() { [[ "$(obs_f '.over_threshold')" == false ]]; }
c_alert_n() { [[ "$(alerts_n)" == "$1" ]]; }
c_open_q_n() { [[ "$(open_q_n)" == "$1" ]]; }
c_empty() { [[ "$(rw | jq_r '.observations | length')" == 0 ]]; }
c_has_obs() { [[ "$(rw | jq_r '.observations | length')" -ge 1 ]]; }

chk_bool() { [[ "$1" == "$2" ]]; } # chk_bool <got> <want>

# ============================================================================
say "request-watch live check — $(now) — binary: $BINARY ($VERSION)"
say "ports: http $PORT tls $TLS_PORT mcp $MCP_PORT, fake node $NODE_PORT"

LEFT_SNAP="$S/leftover.snap"
env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" snapshot "$LEFT_SNAP" >/dev/null 2>&1 || true

CUR=0
git -C "$WORKDIR" init -q 2>/dev/null
git -C "$WORKDIR" config user.email "request-watch-live-check@example.invalid"
git -C "$WORKDIR" config user.name "request watch live check"
printf 'work\n' > "$WORKDIR/README.md"
git -C "$WORKDIR" add README.md && git -C "$WORKDIR" commit -q -m "Initial commit" 2>/dev/null

write_fake_node
write_worker
seed_store
write_config

if ! start_fake_node; then
  say "FAIL: the fake node did not come up: $(cat "$NODE_ERR" 2>/dev/null | tail -3)"
  exit 1
fi
say "  fake node pid $FAKE_PID on 127.0.0.1:$NODE_PORT"

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
begin_check 1 "Config: the eleven settings"
KEYS=(enabled poll_seconds no_progress_minutes observe_workers observe_verifiers use_gpu_signal gpu_busy_percent unreachable_counts raise_alert raise_question remediation_options)
DEFAULTS='{"enabled":true,"poll_seconds":30,"no_progress_minutes":30,"observe_workers":true,"observe_verifiers":true,"use_gpu_signal":true,"gpu_busy_percent":10,"unreachable_counts":true,"raise_alert":true,"raise_question":true,"remediation_options":["keep_waiting","end_worker","retry_other_backend","unload_reload_model"]}'
RWC() { api GET /api/config | jq -cS '.autonomous.request_watch'; }
RWA() { api GET /api/autonomous/config | jq -cS '.request_watch'; }
D_CFG="$(RWC)"; D_AUT="$(RWA)"; D_WANT="$(printf '%s' "$DEFAULTS" | jq -cS .)"
[[ "$D_CFG" == "$D_WANT" ]]; ck 1 $? "a fresh config reads the documented defaults on GET /api/config (all eleven keys)" "read: \`$D_CFG\`"
[[ "$D_AUT" == "$D_WANT" ]]; ck 1 $? "a fresh config reads the same defaults on the Automata settings route" "read: \`$D_AUT\`"
NKEYS="$(printf '%s' "$D_CFG" | jq_r 'keys | length')"
[[ "$NKEYS" == 11 ]]; ck 1 $? "the block has exactly eleven keys" "keys: $(printf '%s' "$D_CFG" | jq -c 'keys')"

# PUT /api/config with a non-default value for every key, then read both routes.
SET_BODY='{"autonomous.request_watch.enabled":false,"autonomous.request_watch.poll_seconds":7,"autonomous.request_watch.no_progress_minutes":45,"autonomous.request_watch.observe_workers":false,"autonomous.request_watch.observe_verifiers":false,"autonomous.request_watch.use_gpu_signal":false,"autonomous.request_watch.gpu_busy_percent":55,"autonomous.request_watch.unreachable_counts":false,"autonomous.request_watch.raise_alert":false,"autonomous.request_watch.raise_question":false,"autonomous.request_watch.remediation_options":["end_worker"]}'
SET_WANT='{"enabled":false,"poll_seconds":7,"no_progress_minutes":45,"observe_workers":false,"observe_verifiers":false,"use_gpu_signal":false,"gpu_busy_percent":55,"unreachable_counts":false,"raise_alert":false,"raise_question":false,"remediation_options":["keep_waiting","end_worker"]}'
SET_WANT="$(printf '%s' "$SET_WANT" | jq -cS .)"
CODE="$(put_cfg "$SET_BODY")"; G1="$(RWC)"; G2="$(RWA)"
[[ "$CODE" == 200 && "$G1" == "$SET_WANT" ]]; ck 1 $? "PUT then GET /api/config round-trips all eleven keys (non-default values)" "PUT HTTP $CODE; read: \`$G1\`"
[[ "$G2" == "$SET_WANT" ]]; ck 1 $? "the Automata settings route reads the same values" "read: \`$G2\`"
# remediation_options: an explicit empty list is kept distinct from unset.
CODE="$(put_cfg '{"autonomous.request_watch.remediation_options":[]}')"; E1="$(RWC | jq -c '.remediation_options')"
CODE2="$(put_cfg '{"autonomous.request_watch.remediation_options":null}')"; E2="$(RWC | jq -c '.remediation_options')"
[[ "$CODE" == 200 && "$E1" == '["keep_waiting"]' && "$CODE2" == 200 && "$E2" == '["keep_waiting","end_worker","retry_other_backend","unload_reload_model"]' ]]
ck 1 $? "remediation_options: an explicit empty list reads keep_waiting only, null clears it back to unset (all four)" "empty list → \`$E1\` (HTTP $CODE); null → \`$E2\` (HTTP $CODE2)"
# The typed settings route, other values.
SET2='{"request_watch":{"enabled":true,"poll_seconds":9,"no_progress_minutes":12,"observe_workers":true,"observe_verifiers":false,"use_gpu_signal":true,"gpu_busy_percent":25,"unreachable_counts":true,"raise_alert":true,"raise_question":false,"remediation_options":["retry_other_backend","unload_reload_model"]}}'
SET2_WANT='{"enabled":true,"poll_seconds":9,"no_progress_minutes":12,"observe_workers":true,"observe_verifiers":false,"use_gpu_signal":true,"gpu_busy_percent":25,"unreachable_counts":true,"raise_alert":true,"raise_question":false,"remediation_options":["keep_waiting","retry_other_backend","unload_reload_model"]}'
SET2_WANT="$(printf '%s' "$SET2_WANT" | jq -cS .)"
CODE="$(api_code PUT /api/autonomous/config "$SET2")"; H1="$(RWA)"; H2="$(RWC)"
[[ "$CODE" == 200 && "$H1" == "$SET2_WANT" && "$H2" == "$SET2_WANT" ]]; ck 1 $? "PUT on the Automata settings route round-trips all eleven keys and GET /api/config agrees" "PUT HTTP $CODE; settings route: \`$H1\`; /api/config: \`$H2\`"
# Bad values: 400 naming the key; nothing saved.
BEFORE_BAD="$(RWC)"
BAD_OK=0; BAD_TXT=""
bad() { # bad <path> <body> <key that the text must name>
  local out; out="$(api_both PUT "$1" "$2")"
  BAD_TXT+="${3}: ${out:0:170} | "
  [[ "$out" == 400* && "$out" == *"$3"* ]] || BAD_OK=1
}
bad /api/config '{"autonomous.request_watch.poll_seconds":0}' poll_seconds
bad /api/config '{"autonomous.request_watch.no_progress_minutes":"soon"}' no_progress_minutes
bad /api/config '{"autonomous.request_watch.gpu_busy_percent":101}' gpu_busy_percent
bad /api/config '{"autonomous.request_watch.enabled":"maybe"}' enabled
bad /api/config '{"autonomous.request_watch.remediation_options":["keep_waiting","reboot_node"]}' remediation_options
bad /api/config '{"autonomous.request_watch.raise_alert":7,"autonomous.request_watch.poll_seconds":3}' raise_alert
bad /api/autonomous/config '{"request_watch":{"poll_seconds":-4}}' poll_seconds
bad /api/autonomous/config '{"request_watch":{"no_such_key":true}}' no_such_key
ck 1 $BAD_OK "a bad value is a 400 that names the key, on both routes (eight refused requests)" "$BAD_TXT"
AFTER_BAD="$(RWC)"
[[ "$AFTER_BAD" == "$BEFORE_BAD" ]]; ck 1 $? "the refused writes saved nothing, including the valid key sent beside a bad one" "unchanged: \`$AFTER_BAD\`"
# The values this check runs with: a short poll and a one-minute threshold; the rest default.
RUN_BODY='{"autonomous.request_watch.enabled":true,"autonomous.request_watch.poll_seconds":2,"autonomous.request_watch.no_progress_minutes":1,"autonomous.request_watch.observe_workers":true,"autonomous.request_watch.observe_verifiers":true,"autonomous.request_watch.use_gpu_signal":true,"autonomous.request_watch.gpu_busy_percent":10,"autonomous.request_watch.unreachable_counts":true,"autonomous.request_watch.raise_alert":true,"autonomous.request_watch.raise_question":true,"autonomous.request_watch.remediation_options":null}'
CODE="$(put_cfg "$RUN_BODY")"; RUN_NOW="$(RWC)"
RUN_WANT="$(printf '%s' "$DEFAULTS" | jq -cS ".poll_seconds=$POLL_SECONDS | .no_progress_minutes=$NO_PROGRESS_MINUTES")"
[[ "$CODE" == 200 && "$RUN_NOW" == "$RUN_WANT" ]]; ck 1 $? "the run's own settings are set through PUT /api/config: poll_seconds $POLL_SECONDS, no_progress_minutes $NO_PROGRESS_MINUTES, the rest at their defaults" "PUT HTTP $CODE; read: \`$RUN_NOW\`"

# ============================================================================
# Setup for checks 2 to 7: register the fake node and the LLM entry, approve and
# run the Automaton; its task session lands on the node.
CUR=0
NODE_CODE="$(api_code POST /api/compute/nodes "{\"name\":\"$NODE_NAME\",\"kind\":\"ollama\",\"address\":\"127.0.0.1:$NODE_PORT\"}")"
LLM_BODY="$(api_both POST /api/llms "{\"name\":\"$LLM_NAME\",\"kind\":\"shell\",\"model\":\"$MODEL_NAME\",\"compute_nodes\":[\"$NODE_NAME\"]}")"
say "  register node HTTP $NODE_CODE; register llm ${LLM_BODY:0:160}"
CODE_A="$(api_code POST "/api/autonomous/prds/$AUTOMATON_ID/approve" '{}')"
CODE_R="$(api_code POST "/api/autonomous/prds/$AUTOMATON_ID/run" '{}')"
say "  approve $CODE_A, run $CODE_R"
wait_until 150 c_has_obs; PLACED=$?
SID="$(obs_f '.session_id')"
say "  placed=$PLACED session=$SID at $(now)"
if [[ $PLACED != 0 ]]; then
  say "  DIAG task status: $(task_status); sessions: $(api GET /api/sessions | jq -c '[.[]? | {id, name, state, backend: .backend_family, node: .compute_node_ref}]')"
  dlog | grep -iE "$AUTOMATON_ID|request.?watch|compute|livecheck" | tail -15 | cut -c1-260 | sed 's/^/    log: /'
fi
SESS_JSON="$(api GET "/api/sessions/$SID")"
rep ""
rep "Placement: the Automaton \`$AUTOMATON_ID\` names the LLM entry \`$LLM_NAME\` (kind shell, one compute node \`$NODE_NAME\` of kind ollama at the fake node's address) as its backend, so the daemon's executor started the task session on that node. The session JSON: \`compute_node_ref\` \`$(printf '%s' "$SESS_JSON" | jq_r '.compute_node_ref')\`, \`model\` \`$(printf '%s' "$SESS_JSON" | jq_r '.model')\`, state \`$(printf '%s' "$SESS_JSON" | jq_r '.state')\`."

# ============================================================================
begin_check 2 "Generating: every read surface agrees"
node_mode generating
wait_until 90 c_obs_state generating; GEN=$?
GEN_OBS="$(obs)"
rep_block "The observation (\`GET /api/autonomous/request-watch\`, the first entry):" "$(printf '%s' "$GEN_OBS" | jq .)"
ck 2 $GEN "with the fake node generating the state is generating" "state \`$(printf '%s' "$GEN_OBS" | jq_r '.state')\`, summary \`$(printf '%s' "$GEN_OBS" | jq_r '.summary')\`"
printf '%s' "$GEN_OBS" | jq -e --arg a "$AUTOMATON_ID" --arg t "$TASK_ID" --arg n "$NODE_NAME" --arg m "$MODEL_NAME" \
  '.automaton_id==$a and .task_id==$t and .role=="worker" and .node==$n and .model==$m and (.session_id|length)>0 and (.last_change|length)>0 and (.last_poll|length)>0 and (.no_progress_seconds|type=="number") and .over_threshold==false' >/dev/null 2>&1
ck 2 $? "the observation names the Automaton, task, role worker, node and model, with last_change, last_poll and over_threshold false" "$(printf '%s' "$GEN_OBS" | jq -c '{automaton_id, task_id, role, node, model, over_threshold, no_progress_seconds}')"
[[ "$(printf '%s' "$GEN_OBS" | jq_r '.summary')" == "waiting on a model request: generating" ]]; ck 2 $? "the summary reads 'waiting on a model request: generating'" "summary \`$(printf '%s' "$GEN_OBS" | jq_r '.summary')\`"
printf '%s' "$GEN_OBS" | jq -e '.last_change | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:]+(Z|[+-][0-9:]+)$")' >/dev/null 2>&1; ck 2 $? "last_change is RFC 3339" "last_change \`$(printf '%s' "$GEN_OBS" | jq_r '.last_change')\`"
REST_CODE="$(api_code GET /api/autonomous/request-watch)"; POST_CODE="$(api_code POST /api/autonomous/request-watch '{}')"
[[ "$REST_CODE" == 200 && "$POST_CODE" == 405 ]]; ck 2 $? "the REST read answers 200 and a POST is a 405" "GET HTTP $REST_CODE, POST HTTP $POST_CODE"

SURF_PY="$S/surfaces.py"
cat > "$SURF_PY" <<'PY'
import json, sys
rest_p, cli_p, mcp_p, chat_p, cli_text_p, want_state = sys.argv[1:7]
rest, cli, mcp, chat, cli_text = [open(p).read() for p in (rest_p, cli_p, mcp_p, chat_p, cli_text_p)]
problems = []
try:
    r = json.loads(rest); c = json.loads(cli); m = json.loads(mcp)
except Exception as e:
    print("a read did not return JSON: REST %r, CLI %r, MCP %r" % (rest[:50].strip(), cli[:50].strip(), mcp[:50].strip())); sys.exit(1)
keep = ("session_id", "automaton_id", "task_id", "role", "node", "model", "state", "summary")
def view(d):
    return [{k: o.get(k) for k in keep} for o in d.get("observations", [])]
if not view(r): problems.append("REST holds no observation")
if view(r) != view(c): problems.append("CLI --json differs from REST")
if view(r) != view(m): problems.append("MCP result differs from REST")
for o in r.get("observations", []):
    if o["state"] != want_state: problems.append("REST state is %s, want %s" % (o["state"], want_state))
    line = "%s %s task %s on %s: %s" % (o["session_id"], o["role"], o["task_id"], o["node"], o["summary"])
    if line not in chat: problems.append("chat reply misses the line for " + o["session_id"])
    if line not in cli_text: problems.append("CLI text misses the line for " + o["session_id"])
print("OK" if not problems else "; ".join(problems))
sys.exit(0 if not problems else 1)
PY
mcp_read() { # prints the autonomous_request_watch result text (the REST bridge of the MCP server)
  api POST /api/mcp/call '{"tool":"autonomous_request_watch","params":{}}' | jq -r '.content[0].text // empty' 2>/dev/null
}
SV=""
for try in 1 2 3; do
  rw > "$S/s-rest.json"
  dw autonomous request-watch --json > "$S/s-cli.json" 2> "$S/s-cli.err"
  dw autonomous request-watch > "$S/s-cli.txt" 2>> "$S/s-cli.err"
  mcp_read > "$S/s-mcp.json"
  api POST /api/test/message '{"text":"autonomous request-watch"}' | jq -r '.responses | join("\n")' > "$S/s-chat.txt" 2>/dev/null
  SV="$("$PY" -I "$SURF_PY" "$S/s-rest.json" "$S/s-cli.json" "$S/s-mcp.json" "$S/s-chat.txt" "$S/s-cli.txt" generating 2>&1)"
  [[ "$SV" == OK ]] && break
  sleep 3
done
rep_block "CLI text (\`datawatch autonomous request-watch\`):" "$(cat "$S/s-cli.txt")"
rep_block "Chat reply (\`POST /api/test/message\`, text \`autonomous request-watch\`):" "$(cat "$S/s-chat.txt")"
rep_block "MCP result (\`autonomous_request_watch\` through \`POST /api/mcp/call\`, first 600 bytes):" "$(head -c 600 "$S/s-mcp.json")"
[[ "$SV" == OK ]]; ck 2 $? "REST, MCP, CLI (--json and text) and the chat verb all report generating and agree" "comparison: \`$SV\`"
F_REST="$(rw | jq_r '.observations | length')"; F_ONE="$(api GET "/api/autonomous/request-watch?session=$SID" | jq_r '.observations | length')"
F_AUT="$(api GET "/api/autonomous/request-watch?automaton=$AUTOMATON_ID" | jq_r '.observations | length')"; F_NONE="$(api GET "/api/autonomous/request-watch?session=nosuch" | jq_r '.observations | length')"
F_CLI="$(dw autonomous request-watch --session "$SID" --json 2>/dev/null | jq_r '.observations | length')"
[[ "$F_REST" == 1 && "$F_ONE" == 1 && "$F_AUT" == 1 && "$F_NONE" == 0 && "$F_CLI" == 1 ]]; ck 2 $? "the session and automaton filters narrow the list (an unknown id reads empty), on REST and the CLI" "all $F_REST; ?session=<id> $F_ONE; ?automaton=<id> $F_AUT; ?session=nosuch $F_NONE; CLI --session $F_CLI"
SMR="$(api GET "/api/sessions/$SID" | jq -c '.model_request // {}')"
[[ "$(printf '%s' "$SMR" | jq_r '.state')" == generating && "$(printf '%s' "$SMR" | jq_r '.session_id')" == "$SID" ]]; ck 2 $? "the session JSON (GET /api/sessions/<id>) carries model_request, generating" "$(printf '%s' "$SMR" | jq -c '{session_id, state, summary}')"
SMRL="$(api GET /api/sessions | jq_r --arg s "$SID" '[.[]? | select(.full_id==$s or .id==$s)][0].model_request.state')"
[[ "$SMRL" == generating ]]; ck 2 $? "the session list carries model_request too" "state \`$SMRL\`"
TMR="$(api GET "/api/autonomous/prds/$AUTOMATON_ID?detail=tasks" | jq -c --arg t "$TASK_ID" '[.tasks[]? | select(.id==$t)][0].model_request // {}')"
[[ "$(printf '%s' "$TMR" | jq_r '.state')" == generating ]]; ck 2 $? "the task view (?detail=tasks) carries model_request on the task" "$(printf '%s' "$TMR" | jq -c '{task_id, role, state, summary}')"
FMR="$(prd | jq -c --arg t "$TASK_ID" '[.stories[]?.tasks[]? | select(.id==$t)][0].model_request // {}')"
[[ "$(printf '%s' "$FMR" | jq_r '.state')" == generating ]]; ck 2 $? "the full Automaton record carries model_request on the task as well" "$(printf '%s' "$FMR" | jq -c '{task_id, state}')"
SESS_STATE_0="$(sess_state)"; TASK_STATUS_0="$(task_status)"
[[ "$SESS_STATE_0" == running && "$(task_flight)" == yes ]]; ck 2 $? "the worker session is running and its task is in flight (in progress or verifying: the request has not ended)" "session \`$SESS_STATE_0\`, task \`$TASK_STATUS_0\`"

# ============================================================================
begin_check 3 "Idle and threshold: one alert and one question"
ALERTS_0="$(alerts_n)"; QOPEN_0="$(open_q_n)"
node_mode idle
wait_until 30 c_obs_state idle; IDLE=$?
ck 3 $IDLE "with the node idle (the same body on every read) the state is idle" "state \`$(obs_f '.state')\`"
NP1="$(obs_f '.no_progress_seconds')"; S1="$(obs_f '.summary')"; sleep 7; NP2="$(obs_f '.no_progress_seconds')"; S2="$(obs_f '.summary')"
[[ "$NP2" -gt "$NP1" ]]; ck 3 $? "the no-progress time grows while the node stays idle" "no_progress_seconds $NP1 then $NP2 (7 seconds apart)"
[[ "$S1" == "waiting on a model request: no progress for "* && "$S2" == "waiting on a model request: no progress for "* ]]; ck 3 $? "the summary reads 'waiting on a model request: no progress for ...'" "summary \`$S2\`"
[[ "$(obs_f '.over_threshold')" == false && "$(alerts_n)" == "$ALERTS_0" && "$(open_q_n)" == "$QOPEN_0" ]]; ck 3 $? "below the threshold there is no alert and no question yet" "over_threshold \`$(obs_f '.over_threshold')\`, alerts $(alerts_n), open questions $(open_q_n)"
say "  waiting for the $NO_PROGRESS_MINUTES-minute threshold (started $(now))"
wait_until 150 c_over; OV=$?
ck 3 $OV "past one minute without progress over_threshold turns true" "no_progress_seconds $(obs_f '.no_progress_seconds')"
[[ "$(obs_f '.summary')" =~ ^waiting\ on\ a\ model\ request:\ no\ progress\ for\ [0-9]+\ minutes?$ ]]; ck 3 $? "the summary then reads 'no progress for N minutes'" "summary \`$(obs_f '.summary')\`"
wait_until 20 c_alert_n $((ALERTS_0 + 1)); wait_until 20 c_open_q_n $((QOPEN_0 + 1))
sleep $((POLL_SECONDS * 3 + 2))
ALERTS_1="$(alerts_n)"; QOPEN_1="$(open_q_n)"
[[ "$ALERTS_1" == $((ALERTS_0 + 1)) ]]; ck 3 $? "exactly one alert was raised, and a poll later still exactly one" "alerts titled '$ALERT_TITLE': $ALERTS_0 before, $ALERTS_1 after three more polls"
[[ "$QOPEN_1" == $((QOPEN_0 + 1)) ]]; ck 3 $? "exactly one question was raised, and a poll later still exactly one" "open request-watch questions: $QOPEN_0 before, $QOPEN_1 after"
ALERT_JSON="$(api GET /api/alerts | jq -c --arg t "$ALERT_TITLE" '[.alerts[]? | select(.title==$t)][0]')"
rep_block "The alert (\`GET /api/alerts\`):" "$(printf '%s' "$ALERT_JSON" | jq '{level, title, body}')"
printf '%s' "$ALERT_JSON" | jq -e --arg a "$AUTOMATON_ID" --arg n "$NODE_NAME" '.level=="warn" and (.body|contains($a)) and (.body|contains($n)) and (.body|contains("No progress for: 1 minute (limit 1 minute)"))' >/dev/null 2>&1
ck 3 $? "the alert names the Automaton, the node and the minutes without progress (\"1 minute\", singular)" "level \`$(printf '%s' "$ALERT_JSON" | jq_r '.level')\`"
Q1="$(open_q | jq -c '.[0] // {}')"; QID="$(printf '%s' "$Q1" | jq_r '.id')"
QJ="$(api GET "/api/questions/$QID" | jq '.question // .')"
rep_block "The question (\`GET /api/questions/<id>\`):" "$(printf '%s' "$QJ" | jq '{id, asked_by, label, state, default, blocking: (.blocking // false), task_id, prd_id, options: [.options[]? | .id], text}')"
printf '%s' "$QJ" | jq -e '[.options[].id] == ["keep_waiting","end_worker","retry_other_backend","unload_reload_model"]' >/dev/null 2>&1
ck 3 $? "the question has four options, keep_waiting first" "options $(printf '%s' "$QJ" | jq -c '[.options[]?.id]')"
[[ "$(printf '%s' "$QJ" | jq_r '.default')" == keep_waiting && "$(printf '%s' "$QJ" | jq_r '.blocking // false')" == false && "$(printf '%s' "$QJ" | jq_r '.state')" == open ]]
ck 3 $? "keep_waiting is the default and the question is not blocking" "default \`$(printf '%s' "$QJ" | jq_r '.default')\`, blocking \`$(printf '%s' "$QJ" | jq_r '.blocking // false')\`, state \`$(printf '%s' "$QJ" | jq_r '.state')\`"
[[ "$(printf '%s' "$QJ" | jq_r '.prd_id')" == "$AUTOMATON_ID" && "$(printf '%s' "$QJ" | jq_r '.task_id')" == "$TASK_ID" ]]; ck 3 $? "the question belongs to the Automaton and task" "prd \`$(printf '%s' "$QJ" | jq_r '.prd_id')\`, task \`$(printf '%s' "$QJ" | jq_r '.task_id')\`"
[[ "$(sess_state)" == running && "$(task_status)" == "$TASK_STATUS_0" ]]; ck 3 $? "raising the question did not park or change the task or the session" "session \`$(sess_state)\`, task \`$(task_status)\`"

# ============================================================================
begin_check 4 "Answer records only"
DEC_0="$(prd | jq_r '[.decisions[]? | select(.kind=="request_watch_answer")] | length')"
LINES_BEFORE="$(node_log_lines)"; SESS_BEFORE="$(sess_state)"; TASK_BEFORE="$(task_status)"
CODE="$(api_code POST "/api/questions/$QID/answer" '{"chosen_option_id":"end_worker","actor":"operator","surface":"cli"}')"
sleep $((POLL_SECONDS * 3 + 2))
DEC_1="$(prd | jq_r '[.decisions[]? | select(.kind=="request_watch_answer")] | length')"
DEC_J="$(prd | jq -c '[.decisions[]? | select(.kind=="request_watch_answer")][0] // {}')"
rep_block "The recorded decision (\`GET /api/autonomous/prds/<id>\`, decisions):" "$(printf '%s' "$DEC_J" | jq '{kind, actor, decision_state, note}')"
[[ "$CODE" == 2?? ]]; ck 4 $? "the answer (end_worker) is accepted" "HTTP $CODE"
[[ "$DEC_0" == 0 && "$DEC_1" == 1 ]]; ck 4 $? "exactly one request_watch_answer decision was recorded" "decisions of that kind: $DEC_0 before, $DEC_1 after"
[[ "$(printf '%s' "$DEC_J" | jq_r '.note')" == *end_worker* && "$(printf '%s' "$DEC_J" | jq_r '.note')" == *"performed no remediation"* ]]; ck 4 $? "the decision names the choice and says nothing was performed" "note \`$(printf '%s' "$DEC_J" | jq_r '.note' | cut -c1-200)\`"
[[ "$(api GET "/api/questions/$QID" | jq_r '.question.state // .state')" == answered ]]; ck 4 $? "the question is answered" "state \`$(api GET "/api/questions/$QID" | jq_r '.question.state // .state')\`"
SESS_AFTER="$(sess_state)"; TASK_AFTER="$(task_status)"
[[ "$SESS_AFTER" == running && "$SESS_BEFORE" == running ]]; ck 4 $? "the worker session is still running after the answer (end_worker ended nothing)" "session \`$SESS_BEFORE\` before, \`$SESS_AFTER\` after"
[[ "$TASK_AFTER" == "$TASK_BEFORE" ]]; ck 4 $? "the task status is unchanged" "task \`$TASK_BEFORE\` before, \`$TASK_AFTER\` after"
[[ "$(obs_f '.state')" == idle && "$(obs_f '.session_id')" == "$SID" ]]; ck 4 $? "the watch still observes the same session (nothing was retried elsewhere)" "state \`$(obs_f '.state')\`, session \`$(obs_f '.session_id')\`"
NEW_REQ="$(tail -n +"$((LINES_BEFORE + 1))" "$NODE_LOG" 2>/dev/null)"
ALL_REQ="$(cat "$NODE_LOG")"
rep_block "Fake node request log since the answer (method and path), then every distinct request of the whole run with its count:" "$(printf '%s\n' "$NEW_REQ" | sort | uniq -c; echo ---; printf '%s\n' "$ALL_REQ" | sort | uniq -c)"
NEW_N="$(printf '%s\n' "$NEW_REQ" | grep -c . || true)"; NEW_PS="$(printf '%s\n' "$NEW_REQ" | grep -cx 'GET /api/ps' || true)"
[[ "$NEW_N" -ge 1 && "$NEW_N" == "$NEW_PS" ]]; ck 4 $? "after the answer the fake node saw only GET /api/ps (the watch keeps reading, nothing else)" "$NEW_PS of $NEW_N requests since the answer were GET /api/ps"
# Over the whole run: GET /api/ps is the watch. The only other request a node may see is
# the compute registry's own reachability probe (a bodiless HEAD /) made when the node
# was registered, before the watch read anything.
node_log_verdict() { # prints "ok <summary>" or "bad <summary>"
  local other heads first_ps first_head
  other="$(grep -vE '^(GET /api/ps|HEAD /)$' "$NODE_LOG" || true)"
  heads="$(grep -cx 'HEAD /' "$NODE_LOG" || true)"
  first_ps="$(grep -nx 'GET /api/ps' "$NODE_LOG" | head -1 | cut -d: -f1)"
  first_head="$(grep -nx 'HEAD /' "$NODE_LOG" | head -1 | cut -d: -f1)"
  if [[ -z "$other" && "${heads:-0}" -le 2 && ( "${heads:-0}" == 0 || ( -n "$first_ps" && "$first_head" -lt "$first_ps" ) || -z "$first_ps" ) ]]; then
    echo "ok $(wc -l < "$NODE_LOG" | tr -d ' ') requests: $(grep -cx 'GET /api/ps' "$NODE_LOG") GET /api/ps, $heads HEAD / (the compute registry's reachability probe at registration)"
  else
    echo "bad $(wc -l < "$NODE_LOG" | tr -d ' ') requests; others: ${other:-none}; HEAD / count ${heads:-0}"
  fi
}
V4="$(node_log_verdict)"
ck 4 "$([[ "$V4" == ok* ]] && echo 0 || echo 1)" "over the whole run the node saw no generate, chat, pull, unload or POST: only GET /api/ps and the registry's own reachability probe at registration" "${V4#* }"

# ============================================================================
begin_check 5 "Recovery: generating again, an open question is withdrawn"
node_mode generating
wait_until 30 c_obs_state generating; RC=$?
ck 5 $RC "with the node generating again the state returns to generating" "state \`$(obs_f '.state')\`, over_threshold \`$(obs_f '.over_threshold')\`"
[[ "$(obs_f '.summary')" == "waiting on a model request: generating" && "$(obs_f '.no_progress_seconds')" -lt 10 ]]; ck 5 $? "the no-progress time starts again from the new progress" "no_progress_seconds $(obs_f '.no_progress_seconds'), summary \`$(obs_f '.summary')\`"
# The first question was answered in check 4, so nothing is open now. A second
# no-progress period raises a second episode's alert and question, and the
# recovery after it withdraws the open one.
ALERTS_2="$(alerts_n)"; QOPEN_2="$(open_q_n)"
node_mode idle
say "  second no-progress period (started $(now))"
wait_until 30 c_obs_state idle
wait_until 150 c_over; OV2=$?
wait_until 20 c_open_q_n $((QOPEN_2 + 1)); wait_until 20 c_alert_n $((ALERTS_2 + 1))
ALERTS_3="$(alerts_n)"; QOPEN_3="$(open_q_n)"
[[ $OV2 == 0 && "$ALERTS_3" == $((ALERTS_2 + 1)) && "$QOPEN_3" == $((QOPEN_2 + 1)) ]]; ck 5 $? "a new no-progress period after recovery is a new episode: one more alert and one open question" "alerts $ALERTS_2 → $ALERTS_3, open questions $QOPEN_2 → $QOPEN_3"
Q2="$(open_q | jq -r '.[0].id // empty')"
[[ -n "$Q2" && "$Q2" != "$QID" ]]; ck 5 $? "the open question is a different one from the answered question" "open \`$Q2\`, answered \`$QID\`"
node_mode generating
wait_until 30 c_obs_state generating; RC2=$?
wait_until 20 c_open_q_n "$QOPEN_2"; WQ=$?
Q2_STATE="$(api GET "/api/questions/$Q2" | jq_r '.question.state // .state')"
ck 5 $RC2 "progress resumes: the state is generating again" "state \`$(obs_f '.state')\`"
[[ $WQ == 0 && "$Q2_STATE" == withdrawn ]]; ck 5 $? "the open question is withdrawn when the request recovers" "question \`$Q2\` state \`$Q2_STATE\`; open request-watch questions now $(open_q_n)"
[[ "$(sess_state)" == running && "$(task_status)" == "$TASK_STATUS_0" ]]; ck 5 $? "the session and the task were left alone throughout" "session \`$(sess_state)\`, task \`$(task_status)\`"

# ============================================================================
begin_check 6 "Unreachable: the node is stopped"
ALERTS_4="$(alerts_n)"; QOPEN_4="$(open_q_n)"
td_stop_one "$FAKE_PID"
FAKE_GONE=1; td_alive "$FAKE_PID" || FAKE_GONE=0
ck 6 $FAKE_GONE "the fake node is ended by its recorded PID" "pid $FAKE_PID alive: $(td_alive "$FAKE_PID" && echo yes || echo no)"
wait_until 30 c_obs_state unreachable; UN=$?
UN_OBS="$(obs)"
rep_block "The observation with the node down:" "$(printf '%s' "$UN_OBS" | jq .)"
ck 6 $UN "with the node stopped the state is unreachable" "state \`$(printf '%s' "$UN_OBS" | jq_r '.state')\`"
[[ -n "$(printf '%s' "$UN_OBS" | jq_r '.error // empty')" ]]; ck 6 $? "the error says why the node could not be read" "error \`$(printf '%s' "$UN_OBS" | jq_r '.error' | cut -c1-160)\`"
[[ "$(printf '%s' "$UN_OBS" | jq_r '.summary')" == "waiting on a model request: node unreachable, no progress for "* ]]; ck 6 $? "the summary reads 'node unreachable, no progress for ...'" "summary \`$(printf '%s' "$UN_OBS" | jq_r '.summary')\`"
UC="$(cfg_get '.autonomous.request_watch.unreachable_counts')"
say "  unreachable_counts=$UC; waiting for the threshold (started $(now))"
wait_until 150 c_over; UO=$?
ck 6 $UO "with unreachable_counts true the unreachable time crosses the threshold" "unreachable_counts \`$UC\`; over_threshold \`$(obs_f '.over_threshold')\`, no_progress_seconds $(obs_f '.no_progress_seconds'), summary \`$(obs_f '.summary')\`"
wait_until 20 c_alert_n $((ALERTS_4 + 1)); wait_until 20 c_open_q_n $((QOPEN_4 + 1))
[[ "$(alerts_n)" == $((ALERTS_4 + 1)) && "$(open_q_n)" == $((QOPEN_4 + 1)) ]]; ck 6 $? "the crossing raised one alert and one question, as an idle request does" "alerts $ALERTS_4 → $(alerts_n), open questions $QOPEN_4 → $(open_q_n)"
CODE="$(put_cfg '{"autonomous.request_watch.unreachable_counts":false}')"
wait_until 30 c_not_over; NO=$?
sleep $((POLL_SECONDS * 2 + 1))
[[ "$CODE" == 200 && $NO == 0 && "$(obs_f '.over_threshold')" == false && "$(obs_f '.state')" == unreachable && "$(obs_f '.no_progress_seconds')" -lt 20 ]]
ck 6 $? "with unreachable_counts false the same unreachable node does not cross the threshold (the no-progress time restarts at every poll)" "PUT HTTP $CODE; over_threshold \`$(obs_f '.over_threshold')\`, no_progress_seconds $(obs_f '.no_progress_seconds'), state \`$(obs_f '.state')\`"
put_cfg '{"autonomous.request_watch.unreachable_counts":true}' >/dev/null

# ============================================================================
begin_check 7 "Disabled and role flags"
put_cfg '{"autonomous.request_watch.enabled":false}' >/dev/null
wait_until 20 c_empty; EN=$?
EN_BODY="$(rw)"
[[ $EN == 0 && "$(printf '%s' "$EN_BODY" | jq_r '.enabled')" == false && "$(printf '%s' "$EN_BODY" | jq -c '.observations')" == '[]' ]]; ck 7 $? "with enabled=false the read is empty" "$(printf '%s' "$EN_BODY" | jq -c '{enabled, observations}')"
[[ "$(api GET "/api/sessions/$SID" | jq -c '.model_request // null')" == null ]]; ck 7 $? "with enabled=false the session JSON carries no model_request" "model_request: $(api GET "/api/sessions/$SID" | jq -c '.model_request // null')"
[[ "$(dw autonomous request-watch 2>&1)" == "No model requests are being watched."* ]]; ck 7 $? "with enabled=false the CLI says no model requests are being watched" "$(dw autonomous request-watch 2>&1 | head -1)"
put_cfg '{"autonomous.request_watch.enabled":true}' >/dev/null
wait_until 30 c_has_obs; BACK=$?
ck 7 $BACK "with enabled=true again the worker is observed again" "observations $(rw | jq_r '.observations | length'), state \`$(obs_f '.state')\`"
put_cfg '{"autonomous.request_watch.observe_workers":false}' >/dev/null
wait_until 20 c_empty; OW=$?
OW_BODY="$(rw)"
[[ $OW == 0 && "$(printf '%s' "$OW_BODY" | jq_r '.enabled')" == true && "$(printf '%s' "$OW_BODY" | jq -c '.observations')" == '[]' ]]; ck 7 $? "with observe_workers=false the worker is hidden (the watch itself stays on)" "$(printf '%s' "$OW_BODY" | jq -c '{enabled, observations}')"
put_cfg '{"autonomous.request_watch.observe_workers":true,"autonomous.request_watch.observe_verifiers":false}' >/dev/null
wait_until 30 c_has_obs; OV3=$?
ck 7 $OV3 "observe_verifiers=false does not hide a worker" "observations $(rw | jq_r '.observations | length'), role \`$(obs_f '.role')\`"
put_cfg '{"autonomous.request_watch.observe_verifiers":true}' >/dev/null
[[ "$(sess_state)" == running && "$(task_status)" == "$TASK_STATUS_0" ]]; ck 7 $? "the session and the task are still untouched" "session \`$(sess_state)\`, task \`$(task_status)\`"
V7="$(node_log_verdict)"
ck 7 "$([[ "$V7" == ok* ]] && echo 0 || echo 1)" "at the end of the run the node's log still holds nothing but GET /api/ps and the registration probe" "${V7#* }"

# ============================================================================
CUR=0
DAEMON_LOG_TAIL="$(dlog | grep -iE 'request.?watch|requestwatch' | cut -c1-200 | tail -12)"

begin_check 8 "Cleanup"
# Delete what the check created, through the API, and confirm it is gone.
api_code DELETE "/api/autonomous/prds/$AUTOMATON_ID" >/dev/null   # cancel: ends the task session
sleep 3
DEL_A_BODY="$(api_both DELETE "/api/autonomous/prds/$AUTOMATON_ID?hard=true" '{}')"; DEL_A="${DEL_A_BODY%% *}"
for sid in $(api GET /api/sessions | jq -r '.[]? | select((.name // "") | startswith("autonomous:")) | .full_id // .id'); do
  api POST /api/sessions/delete "{\"id\":\"$sid\"}" >/dev/null
done
for q in $(open_q | jq -r '.[]?.id'); do api POST "/api/questions/$q/withdraw" '{"actor":"operator","reason":"live check finished"}' >/dev/null; done
DEL_L="$(api_code DELETE "/api/llms/$LLM_NAME")"; DEL_N="$(api_code DELETE "/api/compute/nodes/$NODE_NAME")"
A_GONE="$(api_code GET "/api/autonomous/prds/$AUTOMATON_ID")"; L_GONE="$(api_code GET "/api/llms/$LLM_NAME")"; N_GONE="$(api_code GET "/api/compute/nodes/$NODE_NAME")"
SESS_LEFT="$(api GET /api/sessions | jq_r '[.[]? | select((.name // "") | startswith("autonomous:"))] | length')"
[[ "$DEL_A" == 2?? && "$A_GONE" == 404 ]]; ck 8 $? "the Automaton (and its task) is deleted through the API" "cancel, then hard DELETE HTTP $DEL_A (${DEL_A_BODY:0:120}); GET now HTTP $A_GONE"
[[ "$SESS_LEFT" == 0 ]]; ck 8 $? "the worker session is gone" "autonomous sessions left: $SESS_LEFT"
[[ "$DEL_L" == 2?? && "$L_GONE" == 404 && "$DEL_N" == 2?? && "$N_GONE" == 404 ]]; ck 8 $? "the LLM entry and the compute node are deleted through the API" "llm: DELETE $DEL_L, GET $L_GONE; node: DELETE $DEL_N, GET $N_GONE"
[[ "$(open_q_n)" == 0 ]]; ck 8 $? "no request-watch question is left open" "open: $(open_q_n)"
wait_until 20 c_empty; ck 8 $? "the watch holds no observation once the session is gone" "$(rw | jq -c '.observations')"
td_stop_all
sleep 1
D_GONE="no"; td_alive "$DPID" || D_GONE="yes"
N_PID_GONE="no"; td_alive "$FAKE_PID" || N_PID_GONE="yes"
PORT_CLOSED="no"; td_wait_port_closed "$TLS_PORT" 20 && PORT_CLOSED="yes"
NODE_PORT_CLOSED="no"; td_wait_port_closed "$NODE_PORT" 20 && NODE_PORT_CLOSED="yes"
LEFT_OUT="$(env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" check "$LEFT_SNAP" 2>&1)"; LEFT_RC=$?
[[ "$D_GONE" == yes && "$PORT_CLOSED" == yes ]]; ck 8 $? "the sandbox daemon is gone and its port is closed" "daemon gone: $D_GONE; port $TLS_PORT closed: $PORT_CLOSED"
[[ "$N_PID_GONE" == yes && "$NODE_PORT_CLOSED" == yes ]]; ck 8 $? "the fake node is gone and its port is closed" "node gone: $N_PID_GONE; port $NODE_PORT closed: $NODE_PORT_CLOSED"
# A daemon that is new since the snapshot but whose config is not under this check's
# scratch area belongs to another job on the host, not to this check.
LEFT_OURS="$(printf '%s\n' "$LEFT_OUT" | awk -v s="config=$S/" 'index($0, s) > 0' | wc -l | tr -d ' ')"
LEFT_OTHERS=$(( $(printf '%s\n' "$LEFT_OUT" | grep -c 'leftover test daemon:' || true) - LEFT_OURS ))
[[ "$LEFT_OURS" == 0 ]]; ck 8 $? "no test daemon was left behind (scripts/check-no-leftover-test-daemons.sh against the snapshot taken before the run)" "check exit $LEFT_RC — $(printf '%s' "$LEFT_OUT" | tail -1 | sed 's/\bFAIL\b/failed/g'); new daemons whose config lies in this check's scratch area: $LEFT_OURS; new daemons of other jobs on the host (config elsewhere, not started or ended by this check): $LEFT_OTHERS"
rep ""
rep "- The daemon and the fake node were ended by \`td_stop_all\` and \`td_stop_one\` from \`scripts/test-daemon-lib.sh\` (the recorded pids, config or script path as marker); nothing was ended by name."
rep "- The scratch area (config, data, log, worker, fake node, request log) is removed by the exit handler right after this report is written."

# --- assemble the report ---------------------------------------------------
OVERALL=PASS; TOTAL_FAIL=0
for n in 1 2 3 4 5 6 7 8; do TOTAL_FAIL=$(( TOTAL_FAIL + ${CK_FAIL[$n]:-0} )); done
[[ $TOTAL_FAIL -gt 0 ]] && OVERALL=FAIL
for n in 1 2 3 4 5 6 7 8; do [[ "${CK_TOTAL[$n]:-0}" -gt 0 ]] || { OVERALL=FAIL; CK_FAIL[$n]=1; }; done

{
  printf '# Automaton model request watch — live check on a sandbox daemon\n\n'
  printf '**Date:** %s (run started %s, local time)\n' "$(date '+%Y-%m-%d')" "$DAEMON_UP_AT"
  printf '**Version under test:** %s (binary built from this worktree)\n' "${VERSION:-unknown}"
  printf '**Script:** `scripts/request-watch-live-check.sh` (run as `env -u TMUX -u TMUX_TMPDIR bash scripts/request-watch-live-check.sh`)\n'
  printf '**Scope:** the eleven `autonomous.request_watch` settings, `GET /api/autonomous/request-watch` and its MCP, CLI and comm channel reads, the `model_request` field of the session and task views, the generating, idle and unreachable states, the no-progress threshold with its one alert and one non-blocking question, the answer that only records, the recovery that withdraws an open question, and the enabled and role switches, against a fake compute node of kind ollama.\n\n'
  printf '## Result\n\n**Overall:** %s\n\n' "$OVERALL"
  printf '| # | Check | Result | Assertions |\n|---|---|---|---|\n'
  for n in 1 2 3 4 5 6 7 8; do
    v=PASS; [[ "${CK_FAIL[$n]:-0}" -gt 0 ]] && v=FAIL
    printf '| %s | %s | %s | %s of %s passed |\n' "$n" "${CK_TITLE[$n]}" "$v" "$(( ${CK_TOTAL[$n]:-0} - ${CK_FAIL[$n]:-0} ))" "${CK_TOTAL[$n]:-0}"
  done
  printf '\n'
  printf '## Setup\n\n'
  printf -- '- **Sandbox daemon only.** Started with `td_start` from `scripts/test-daemon-lib.sh` from a config generated from `testdata/datawatch.yaml` by the substitutions the test runner uses (data folder, ports, loopback host, test token; the model addresses point at a closed loopback port). Added: `autonomous.questions.enabled` (the operator question routes are off by default), `server.allowed_project_roots`, and `shell_backend.script_path` (the stand-in worker). The request watch itself was left at its defaults in the file; check 1 shows them, then sets `poll_seconds` %s and `no_progress_minutes` %s through `PUT /api/config`. The production daemon, its service and its ports were not touched, and nothing was ended by name.\n' "$POLL_SECONDS" "$NO_PROGRESS_MINUTES"
  printf -- '- **Ports.** HTTP %s, TLS %s, MCP %s, DNS %s, webhook %s, notify %s, fake node %s, each probed free just before use.\n' "$PORT" "$TLS_PORT" "$MCP_PORT" "$DNS_PORT" "$HOOK_PORT" "$NTFY_PORT" "$NODE_PORT"
  printf -- '- **Session sockets.** The daemon ran with its multiplexer socket directory under the throwaway folder, so no session of the operator was reachable and none was touched. The script itself issues no multiplexer command.\n'
  printf -- '- **Fake compute node.** A small python3 HTTP server (`fake-node.py`, recorded by PID like the daemon) serving `GET /api/ps` only. A control file switches its mode: in the generating mode the expiry time of the loaded model changes on every read, in the idle mode the body is the same every time. Every request (method and path) is logged to a file the checks read. It is registered as the compute node `%s` of kind `ollama` through `POST /api/compute/nodes`, and an LLM entry `%s` (kind `shell`, model `%s`, that node only) through `POST /api/llms`.\n' "$NODE_NAME" "$LLM_NAME" "$MODEL_NAME"
  printf -- '- **Worker.** One Automaton (`%s`, one story, one task) written into the sandbox store before the first start, naming the LLM entry as its backend. Approved and run through the REST API, the daemon'"'"'s executor started the task session on the node. The worker is the shell backend running a generated script that waits until told to stop: a model request that is in flight and never ends. No model is involved anywhere.\n' "$AUTOMATON_ID"
  printf -- '- **Everything** lives under one temporary folder inside the worktree'"'"'s ignored `.datawatch/live-check/` folder, removed on every exit path.\n'
  printf '\n'
  cat "$S/rep-0.md" 2>/dev/null
  for n in 1 2 3 4 5 6 7 8; do cat "$S/rep-$n.md" 2>/dev/null; done
  printf '\n## Daemon log, the request watch lines\n\n```\n%s\n```\n' "$DAEMON_LOG_TAIL"
  printf '\n## Spec amendments and what is not shown live\n\n'
  printf -- '- **The first question is answered in check 4, so check 5 withdraws a second one.** An answered question is closed and cannot be withdrawn. Check 5 therefore lets the node go idle again for another full threshold (a new episode, one more alert and one more question), then restores progress and shows that open question withdrawn. The recovery of the first episode is shown as well (state generating, no-progress time restarted).\n'
  printf -- '- **Unreachable with the setting off is shown by flipping it, not by a second full wait.** The node stays down: with `unreachable_counts` true the unreachable time crosses the threshold, then `PUT /api/config` sets it false and the same unreachable node reads not over the threshold, with the no-progress time restarting at every poll. That is the same decision the tracker takes, driven through the API.\n'
  printf -- '- **The poll interval is read after each tick.** The daemon was started with the default 30 second poll, so the first change to a 2 second poll applies after the sleep already running (at most 30 seconds). The script waits for it.\n'
  printf -- '- **The Automaton was written into the sandbox store, not created through the create route.** The route only makes drafts and a plan needs a planner model, which a sandbox does not have. Approve, run, the answers, every read, every setting and every delete go through the REST API of the running daemon; the session was started by the daemon itself.\n'
  printf -- '- **Four defects the live run found, fixed in the daemon (each has a unit test).** First, the watch took a worker as a target only while its task was `in_progress`, but the executor marks a task `verifying` as soon as its one-shot worker has started and keeps it there while the worker runs, so no real worker was ever observed; the target list now takes the running worker of a task that is in progress or verifying (a worker that has ended is not a target). Second, the session list that the daemon serves (the one with the schedule counts) built its rows without `model_request`, so a list row never carried the observation; it does now. Third, with `enabled` false the REST read was empty at once but the session and task views kept showing the last observation until the next poll (up to 30 seconds by default); the views now honour the switch the same way. Fourth, with a one-minute threshold the alert and the question said "1 minutes"; they now say "1 minute".\n'
  printf -- '- **The task is `verifying` while the worker runs.** That is the executor'"'"'s own status for a task whose worker has started (it verifies once the session ends), so the checks read "in flight" as in progress or verifying and assert that the status does not change.\n'
  printf -- '- **One request other than `GET /api/ps` reaches the node.** The compute registry makes a bodiless `HEAD /` reachability probe when a node is registered; it is the daemon'"'"'s own and happens before the watch reads anything. The checks require nothing else: no generate, chat, pull, unload or POST.\n'
  printf -- '- **Not shown live:** a verifier session (observed the same way as a worker; the role flag is shown with the worker), a GPU reading from an attached observer (the node has none, so only the running-models signal is shown), another node kind (only `ollama` has a signal source), and the PWA card and the mobile app. These are covered by the unit tests of the watch and by the PWA tests; this script drives the daemon through REST, MCP, the command line and chat only.\n'
  printf -- '- **Generality.** The node, the LLM entry, the model, the Automaton and the project are all made up inside the sandbox; nothing names the repository this script lives in, and every threshold is a configuration key.\n'
  printf '\nResult: %s\n' "$OVERALL"
} > "$S/report.md"

# No absolute path and no temporary folder name in the tracked report.
SCRUB="$S/scrub.sed"
{
  printf 's|%s|.datawatch/live-check/run|g\n' "$S"
  printf 's|%s|.|g\n' "$ROOT"
  [[ -n "${HOME:-}" ]] && printf 's|%s|<home>|g\n' "$HOME"
  printf 's|/tmp/[^ `"'"'"')]*|<temp>|g\n'
  printf 's|/home/[^ `"'"'"')]*|<path>|g\n'
} > "$SCRUB"
mkdir -p "$(dirname "$REPORT")"
sed -f "$SCRUB" "$S/report.md" > "$REPORT"

say ""
say "Overall: $OVERALL — report: docs/plans/$(basename "$REPORT")"
[[ "$OVERALL" == PASS ]]
