#!/usr/bin/env bash
# headroom-smoke.sh — live REST smoke for the compute-node headroom surface.
#
# Asserts the headroom measure/get routes are LIVE (no 503), that the
# verifier_slots write gate reads the same store it writes (measure ->
# PUT verifier_slots succeeds), and that the gate's free-memory floor is
# the bound actually enforced (floor raised -> the same write is refused).
#
# Runs one daemon on a scratch port with a fake standalone compute node
# (a single python http server answering GET /api/stats with a memory
# payload and POST /api/chat with 200), never touching the operator
# daemon or the live workspace.
#
# Requirements: go, jq, python3, curl.
#
# Usage:
#   BINARY=$(pwd)/datawatch SMOK_PORT=18930 ./tests/live-smoke/headroom-smoke.sh
#
# Exit 0 when all assertions hold. Prints a VERDICT block at the end.

set -uo pipefail

PORT="${SMOK_PORT:-18930}"
FAKE_PORT=$((PORT + 1))
REPO="$(cd "$(dirname "$0")/../.." && pwd)"
BINARY="${BINARY:-$REPO/datawatch}"
ROOT="${SMOK_ROOT:-${DATAWATCH_HOME:-.datawatch}/headroom-smoke-$(date +%Y%m%d-%H%M%S)}"
mkdir -p "$ROOT"; ROOT="$(cd "$ROOT" && pwd)"
SANDBOX="$ROOT/sandbox"
U="http://127.0.0.1:$PORT"
NODE="fake-node"

log(){ printf '[%s] %s\n' "$(date '+%H:%M:%S')" "$*"; }
die(){ printf '!! %s\n' "$*" >&2; exit 1; }

. "$REPO/scripts/test-daemon-lib.sh"
fake_cleanup(){
  if [ -f "$SANDBOX/fake.pid" ]; then kill "$(cat "$SANDBOX/fake.pid" 2>/dev/null)" 2>/dev/null || true; fi
  return 0
}
TD_EXTRA_CLEANUP=fake_cleanup
td_install_traps "$@" # EXIT / INT / TERM / HUP all end the daemon

# --- 1. binary ---------------------------------------------------------------
[ -x "$BINARY" ] || die "no executable BINARY at $BINARY (build first: go build -o datawatch .)"
mkdir -p "$SANDBOX" "$ROOT/bin"
cp -f "$BINARY" "$ROOT/bin/datawatch" || die "copy binary failed"
DW="$ROOT/bin/datawatch"
log "binary=$BINARY"; log "daemon=$U fake=http://127.0.0.1:$FAKE_PORT"

# --- 2. fake compute node -----------------------------------------------------
# One tiny HTTP server stands in for a standalone compute node:
#   GET  /api/stats -> a memory snapshot (no gpus: memory comes from the
#                      system-memory fallback, so free_mb > 0 is real)
#   POST /api/chat  -> 200 (the per-request footprint probe)
# Per-request drop is ~0 (static snapshot) -> per_request_mb = 0 (the
# estimate is unbounded: the measured free memory covers any cap;
# supported_cap = 0). That keeps the smoke deterministic.
cat > "$SANDBOX/fake_node.py" <<'PY'
import json
from http.server import BaseHTTPRequestHandler, HTTPServer

TOTAL = 8 * 1024**3
USED = 2 * 1024**3

class H(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def _send(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/api/stats":
            self._send(200, {
                "gpu": [],
                "mem": {"used_bytes": USED, "total_bytes": TOTAL},
                "envelopes": [],
            })
        else:
            self._send(404, {"error": "not found"})

    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        if n:
            self.rfile.read(n)
        if self.path == "/api/chat":
            self._send(200, {"message": {"content": "ok"}, "done": True})
        else:
            self._send(404, {"error": "not found"})

HTTPServer(("127.0.0.1", int(__import__("os").environ["FAKE_PORT"])), H).serve_forever()
PY
FAKE_PORT="$FAKE_PORT" python3 "$SANDBOX/fake_node.py" > "$SANDBOX/fake.out" 2>&1 &
echo $! > "$SANDBOX/fake.pid"
up=0
for i in $(seq 1 20); do
  curl -sf "http://127.0.0.1:$FAKE_PORT/api/stats" >/dev/null 2>&1 && { up=1; break; }
  sleep 0.5
done
[ "$up" = 1 ] || die "fake node not up; see $SANDBOX/fake.out"
log "fake node up (pid $(cat "$SANDBOX/fake.pid"))"

# --- 3. scratch config -------------------------------------------------------
cat > "$SANDBOX/config.yaml" <<YAML
hostname: headroom-smoke
data_dir: ./data
log: { level: info }
server:
  enabled: true
  port: $PORT
  https: false
  bind: 127.0.0.1
  host: 127.0.0.1
  basic_auth: false
  tls: false
  mcp: false
compute_nodes:
  - name: $NODE
    kind: remote
    address: http://127.0.0.1:$FAKE_PORT
    monitoring_endpoint: http://127.0.0.1:$FAKE_PORT/api/stats
    node_pinned_model: smoke/mini
    verifier_slots: 1
autonomous:
  enabled: false
YAML

# --- 4. start scratch daemon -------------------------------------------------
if command -v ss >/dev/null 2>&1 && ss -ltn 2>/dev/null | awk '{print $4}' | grep -q ":${PORT}$"; then
  die "port $PORT is busy — kill a leftover daemon before re-running"
fi
log "starting scratch daemon"
pushd "$SANDBOX" >/dev/null
td_start "$DW" "$SANDBOX/config.yaml" "$SANDBOX/daemon.out" || die "scratch daemon failed to start; see $SANDBOX/daemon.out"
popd >/dev/null
DPID="$TD_LAST_PID"
health=0
for i in $(seq 1 30); do
  curl -sf "$U/healthz" >/dev/null 2>&1 && { health=1; break; }
  sleep 1
done
[ "$health" = 1 ] || { die "scratch daemon not healthy; see $SANDBOX/daemon.out"; }
kill -0 "$DPID" 2>/dev/null || die "scratch daemon (pid $DPID) is not alive; see $SANDBOX/daemon.out"
log "daemon up (pid $DPID)"

# --- 5. assertions -----------------------------------------------------------
PASS=0; FAIL=0
check(){ # check <name> <cond-exit> 
  if [ "$2" = 0 ]; then PASS=$((PASS+1)); log "  ok  $1"; else FAIL=$((FAIL+1)); log "  FAIL $1"; fi
}
code(){ curl -s -o "$TMP_BODY" -w '%{http_code}' "$@"; }
TMP_BODY="$SANDBOX/body.json"

log "A) unmeasured node: GET 404, cap write refused (409), measure live (no 503)"
c=$(code "$U/api/compute/nodes/$NODE/headroom")
[ "$c" = 404 ] && b0=0 || b0=1; check "GET headroom -> 404 (never measured)" "$b0"

c=$(code -X PUT "$U/api/compute/nodes/$NODE" -H 'Content-Type: application/json' -d '{"verifier_slots": 2}')
[ "$c" = 409 ] && b1=0 || b1=1; check "PUT verifier_slots=2 before a measurement -> 409" "$b1"
grep -qE "refused.*(no fresh measurement|measurement stale)" "$TMP_BODY" 2>/dev/null && b2=0 || b2=1; check "refusal names the cause (no fresh measurement)" "$b2"

c=$(code -X POST "$U/api/compute/nodes/$NODE/headroom")
[ "$c" = 200 ] && b3=0 || b3=1; check "POST headroom measure -> 200 (wired, not 503)" "$b3"
jq -e '.free_mb > 0' "$TMP_BODY" >/dev/null 2>&1 && b4=0 || b4=1; check "measurement free_mb > 0" "$b4"
jq -e '.fresh == true' "$TMP_BODY" >/dev/null 2>&1 && b5=0 || b5=1; check "measurement fresh == true" "$b5"
jq -e '.min_free_mb == 2048' "$TMP_BODY" >/dev/null 2>&1 && b6=0 || b6=1; check "min_free_mb == 2048 (config default bound)" "$b6"
jq -e '.max_age_hours == 24' "$TMP_BODY" >/dev/null 2>&1 && b7=0 || b7=1; check "max_age_hours == 24 (config default bound)" "$b7"
jq -e '.supported_cap == 0' "$TMP_BODY" >/dev/null 2>&1 && b8=0 || b8=1; check "supported_cap == 0 (estimate unbounded)" "$b8"

log "B) after the measurement: same store, same node — gate now accepts"
# PUT takes a full node document (it re-probes on save unless
# ?probe=skip); fetch the current node, bump the cap, send it back.
c=$(code "$U/api/compute/nodes/$NODE")
jq '.verifier_slots = 2' "$TMP_BODY" > "$SANDBOX/node.json"
c=$(code -X PUT "$U/api/compute/nodes/$NODE?probe=skip" -H 'Content-Type: application/json' --data-binary "@$SANDBOX/node.json")
[ "$c" = 200 ] && c1=0 || c1=1; check "PUT verifier_slots=2 after a fresh measurement -> 200" "$c1"
c=$(code "$U/api/compute/nodes/$NODE")
jq -e '.verifier_slots == 2' "$TMP_BODY" >/dev/null 2>&1 && c2=0 || c2=1; check "node now shows verifier_slots == 2" "$c2"

log "C) GET round-trip and the floor is the enforced bound"
c=$(code "$U/api/compute/nodes/$NODE/headroom")
[ "$c" = 200 ] && d1=0 || d1=1; check "GET headroom -> 200 (stored measurement)" "$d1"
jq -e '.fresh == true and .free_mb > 0' "$TMP_BODY" >/dev/null 2>&1 && d2=0 || d2=1; check "GET: fresh and free_mb > 0" "$d2"

# The live probe's free_mb is host-dependent (observer data, not the fake
# node's static 6 GiB), so a fixed floor like 999999 may sit below it and
# the gate stays green. Instead raise the floor to a sentinel above any
# physical free memory (9e18 MB ~ 9 yottabytes) — the gate MUST refuse a
# cap write regardless of the host, which deterministically proves the
# floor is the bound the gate enforces.
HOGE=9000000000000000000
c=$(code -X PUT "$U/api/config" -H 'Content-Type: application/json' -d "{\"autonomous.verifier_min_free_mem_mb\": $HOGE}")
[ "$c" = 200 ] && e0=0 || e0=1; check "raise min_free_mb to $HOGE via PUT /api/config" "$e0"
c=$(code "$U/api/compute/nodes/$NODE/headroom")
jq -e ".min_free_mb == $HOGE" "$TMP_BODY" >/dev/null 2>&1 && e0b=0 || e0b=1; check "GET headroom now reports the raised floor (config binding live)" "$e0b"
c=$(code "$U/api/compute/nodes/$NODE")
jq '.verifier_slots = 3' "$TMP_BODY" > "$SANDBOX/node.json"
c=$(code -X PUT "$U/api/compute/nodes/$NODE?probe=skip" -H 'Content-Type: application/json' --data-binary "@$SANDBOX/node.json")
[ "$c" = 409 ] && e1=0 || e1=1; check "same fresh measurement, floor now above free memory -> 409" "$e1"
grep -qE "refused.*(shortfall|insufficient free memory)" "$TMP_BODY" 2>/dev/null && e2=0 || e2=1; check "refusal names the cause (floor)" "$e2"

# --- 6. verdict --------------------------------------------------------------
log "--------------------------------------------------"
log "VERDICT: PASS=$PASS FAIL=$FAIL"
if [ "$FAIL" = 0 ]; then
  log "headroom surface is live and consistently bounded: measure, gate and read agree."
  exit 0
else
  log "FAILED — sandbox kept at $SANDBOX (daemon.out, fake.out, body.json)"
  exit 1
fi
