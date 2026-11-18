#!/usr/bin/env bash
# hold-live-check.sh — live check of the operator hold on an Automaton, on a
# throwaway test daemon with its own data directory and port.
#
# Steps (each prints one PASS or FAIL line):
#   1. create a draft Automaton over REST
#   2. hold it with a reason and a short until
#   3. GET /api/autonomous/held lists it with the reason
#   4. /api/autonomous/status shows held_prds 1 and blocked_prds 0
#   5. POST run on the held Automaton is refused
#   6. release it and check it is a draft again
#
# The daemon is started through scripts/test-daemon-lib.sh and ended on every
# exit path by the library's traps. The operator's own daemon is never touched.
#
# Usage: env -u TMUX -u TMUX_TMPDIR bash scripts/hold-live-check.sh
# Env:   TEST_BINARY  daemon binary (default: built from this checkout)
# Exit 0 only when every step passed.

if [ -n "${TMUX:-}${TMUX_TMPDIR:-}" ]; then exec env -u TMUX -u TMUX_TMPDIR bash "$0" "$@"; fi
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/scripts/test-daemon-lib.sh"

ORIG_TMPDIR="${TMPDIR:-/tmp}"
if [ "${#ORIG_TMPDIR}" -gt 24 ]; then ORIG_TMPDIR=/tmp; fi
S="$(mktemp -d "$ORIG_TMPDIR/dw-hold-live.XXXXXX")"
export TD_STATE_DIR="$S/tdstate"
# shellcheck source=/dev/null
source "$LIB"

hold_check_cleanup() {
  # Runs after the shared helper has ended the daemon.
  rm -rf "$S" 2>/dev/null || true
}
# shellcheck disable=SC2034 # read by the shared traps in test-daemon-lib.sh
TD_EXTRA_CLEANUP=hold_check_cleanup
td_install_traps "$@"

for t in curl python3 jq; do
  command -v "$t" >/dev/null 2>&1 || { echo "SKIP: $t is not on PATH"; exit 0; }
done

BINARY=""
if [[ -n "${TEST_BINARY:-}" && -x "${TEST_BINARY:-}" ]]; then
  BINARY="$TEST_BINARY"
elif command -v go >/dev/null 2>&1 && mkdir -p "$S/bin" && (cd "$ROOT" && go build -o "$S/bin/datawatch" ./cmd/datawatch/) >/dev/null 2>&1; then
  BINARY="$S/bin/datawatch"
elif [[ -x "$ROOT/bin/datawatch" ]]; then
  BINARY="$ROOT/bin/datawatch"
else
  BINARY="$(command -v datawatch 2>/dev/null || true)"
fi
[[ -n "$BINARY" ]] || { echo "SKIP: no datawatch binary (TEST_BINARY, a build, bin/datawatch, PATH)"; exit 0; }

free_port() {
  python3 -I -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}
PORT="$(free_port)"; TLS_PORT="$(free_port)"; MCP_PORT="$(free_port)"
DNS_PORT="$(free_port)"; HOOK_PORT="$(free_port)"; NTFY_PORT="$(free_port)"; OLLAMA_PORT="$(free_port)"
TOKEN="dw-test-token-12345"
BASE="https://127.0.0.1:$TLS_PORT"
DATA="$S/data"; PROJ="$S/projects"; DTMP="$S/daemon-tmp"; SOCKDIR="$S/run"
CFG="$S/config.yaml"; LOG="$S/daemon.log"
mkdir -p "$DATA" "$PROJ" "$DTMP" "$SOCKDIR"

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
  -e "s|http://local-node:11434|http://127.0.0.1:$OLLAMA_PORT|g" \
  -e "s|http://localhost:11434|http://127.0.0.1:$OLLAMA_PORT|g" \
  -e '/^compute_nodes:/,/^observer:/{/^observer:/!d}' \
  "$ROOT/testdata/datawatch.yaml" > "$CFG"
sed -i -e "s|^server:\$|server:\n  allowed_project_roots: [\"$PROJ\"]|" "$CFG"

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

FAILS=0
step() { # step <0 for pass> <what>
  if [[ "$1" == 0 ]]; then echo "PASS: $2"; else echo "FAIL: $2"; FAILS=$((FAILS + 1)); fi
}

export TMPDIR="$DTMP" TMUX_TMPDIR="$SOCKDIR"
if ! td_start "$BINARY" "$CFG" "$LOG"; then
  echo "FAIL: the test daemon did not stay up: $(tail -5 "$LOG" 2>/dev/null)"
  exit 1
fi
DPID="$TD_LAST_PID"
td_set_api "$DPID" "$BASE" "$TOKEN"
healthy=1
for _ in $(seq 1 60); do
  if curl -sk -m 3 -o /dev/null -f "$BASE/api/health"; then healthy=0; break; fi
  sleep 2
done
if [[ $healthy != 0 ]]; then
  echo "FAIL: the test daemon does not answer /api/health: $(tail -5 "$LOG" 2>/dev/null)"
  exit 1
fi
echo "test daemon pid $DPID healthy on port $TLS_PORT"

# 1. a draft Automaton over REST
CREATED="$(api POST /api/autonomous/prds "$(jq -n --arg d "$PROJ" '{spec:"Hold live check: a draft that is held and released.",project_dir:$d}')")"
ID="$(printf '%s' "$CREATED" | jq -r '.id // empty' 2>/dev/null)"
STATUS="$(printf '%s' "$CREATED" | jq -r '.status // empty' 2>/dev/null)"
ok=1; [[ -n "$ID" && "$STATUS" == draft ]] && ok=0
step $ok "create a draft Automaton over REST (id '${ID:-none}', status '${STATUS:-none}')"
if [[ -z "$ID" ]]; then
  echo "FAIL: nothing to hold; reply was: $CREATED"
  exit 1
fi

# 2. hold it with a reason and a short until
HELD="$(api POST "/api/autonomous/prds/$ID/hold" '{"reason":"live check: waiting on purpose","condition":"until the check ends","until":"2h","actor":"hold-live-check"}')"
ok=1; [[ "$(printf '%s' "$HELD" | jq -r '.status // empty' 2>/dev/null)" == held \
  && "$(printf '%s' "$HELD" | jq -r '.hold.from // empty' 2>/dev/null)" == draft \
  && -n "$(printf '%s' "$HELD" | jq -r '.hold.until // empty' 2>/dev/null)" ]] && ok=0
step $ok "hold it with a reason and until 2h (status held, from draft, until set)"

# 3. the held group
GROUP="$(api GET /api/autonomous/held)"
ok=1; [[ "$(printf '%s' "$GROUP" | jq -r --arg id "$ID" '[.held[]? | select(.id==$id and .reason=="live check: waiting on purpose")] | length' 2>/dev/null)" == 1 \
  && "$(printf '%s' "$GROUP" | jq -r '.total // 0' 2>/dev/null)" == 1 ]] && ok=0
step $ok "GET /api/autonomous/held lists the Automaton with its reason"

# 4. the status counts
ST="$(api GET /api/autonomous/status)"
HELD_N="$(printf '%s' "$ST" | jq -r '.held_prds // 0' 2>/dev/null)"
BLOCKED_N="$(printf '%s' "$ST" | jq -r '.blocked_prds // 0' 2>/dev/null)"
ok=1; [[ "$HELD_N" == 1 && "$BLOCKED_N" == 0 ]] && ok=0
step $ok "status shows held_prds $HELD_N (want 1) and blocked_prds $BLOCKED_N (want 0)"

# 5. run is refused while held
RUN_CODE="$(api_code POST "/api/autonomous/prds/$ID/run" '{}')"
AFTER_RUN="$(api GET "/api/autonomous/prds/$ID" | jq -r '.status // empty' 2>/dev/null)"
ok=1; [[ "$RUN_CODE" =~ ^4[0-9][0-9]$ && "$AFTER_RUN" == held ]] && ok=0
step $ok "run on the held Automaton is refused (HTTP $RUN_CODE, status still '$AFTER_RUN')"

# 6. release it
REL="$(api POST "/api/autonomous/prds/$ID/release_hold" '{"actor":"hold-live-check","note":"live check done"}')"
BACK="$(api GET "/api/autonomous/prds/$ID" | jq -r '.status // empty' 2>/dev/null)"
ok=1; [[ "$(printf '%s' "$REL" | jq -r '.status // empty' 2>/dev/null)" == draft && "$BACK" == draft \
  && "$(api GET /api/autonomous/held | jq -r '.total // 0' 2>/dev/null)" == 0 ]] && ok=0
step $ok "release it: it is a draft again (status '$BACK') and the held list is empty"

if [[ $FAILS -ne 0 ]]; then
  echo "RESULT: FAIL ($FAILS step(s) failed)"
  exit 1
fi
echo "RESULT: PASS"
