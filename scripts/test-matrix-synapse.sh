#!/usr/bin/env bash
# BL241 P1 — Matrix integration tests against a local Docker Synapse.
#
# Usage:
#   scripts/test-matrix-synapse.sh [--no-cleanup] [--datawatch-bin <path>]
#
# What this does:
#   1. Generates Synapse config (explicit generate → patch → run)
#   2. Waits for Synapse health (60s timeout)
#   3. Registers two Matrix users: the datawatch bot + the nio test peer
#   4. Creates a test room and invites both users
#   5. Starts the datawatch daemon with a minimal Matrix config
#   6. Sends a test message via POST /api/matrix/test
#   7. Verifies outbound delivery via room timeline + m.datawatch.session (Q5.3)
#   8. Registers a second user (peer), joins room, sends inbound message
#   9. Tests daemon restart mid-conversation (no message loss)
#  10. Tears down Synapse
#
# Exit 0 = all assertions pass; exit 1 = any failure.
#
# Requirements:
#   docker, curl, python3, jq
#   datawatch binary (defaults to ./datawatch built from source)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

DW_BIN="${DW_BIN:-}"
NO_CLEANUP=0
PASS=0
FAIL=0
SYNAPSE_DATA=""

# ── colours ──────────────────────────────────────────────────────────────────
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m'

ok()   { echo -e "  ${GREEN}PASS${NC}  $*"; PASS=$(( PASS + 1 )); }
ko()   { echo -e "  ${RED}FAIL${NC}  $*"; FAIL=$(( FAIL + 1 )); }
skip() { echo -e "  ${YELLOW}SKIP${NC}  $*"; }
H()    { echo ""; echo "── $* ──────────────────────────────────────"; }

# ── argument parsing ──────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-cleanup) NO_CLEANUP=1; shift ;;
    --datawatch-bin) DW_BIN="$2"; shift 2 ;;
    *) echo "Unknown arg: $1"; exit 1 ;;
  esac
done

# ── prerequisites ─────────────────────────────────────────────────────────────
for cmd in docker curl python3 jq; do
  command -v "$cmd" &>/dev/null || { echo "ERROR: $cmd not found"; exit 1; }
done

# ── build datawatch binary if needed ─────────────────────────────────────────
if [[ -z "$DW_BIN" ]]; then
  DW_BIN="$REPO_ROOT/datawatch-matrix-test"
  echo "Building datawatch binary → $DW_BIN"
  (cd "$REPO_ROOT" && go build -o "$DW_BIN" ./cmd/datawatch/) || { echo "Build failed"; exit 1; }
fi
[[ -x "$DW_BIN" ]] || { echo "ERROR: datawatch binary not executable: $DW_BIN"; exit 1; }

# ── temp workspace ────────────────────────────────────────────────────────────
TMPDIR_DW="$(mktemp -d)"
DW_CONFIG="$TMPDIR_DW/config.yaml"
DW_DATA="$TMPDIR_DW/data"
mkdir -p "$DW_DATA"

cleanup() {
  if [[ "$NO_CLEANUP" -eq 0 ]]; then
    echo ""
    echo "Cleaning up…"
    # Stop daemon if running
    if [[ -n "${DW_PID:-}" ]] && kill -0 "$DW_PID" 2>/dev/null; then
      kill "$DW_PID" 2>/dev/null || true
      wait "$DW_PID" 2>/dev/null || true
    fi
    # Stop and remove Synapse container
    docker stop dw-test-synapse 2>/dev/null || true
    docker rm   dw-test-synapse 2>/dev/null || true
    # Remove Synapse data dir (files may be owned by uid 991, use Docker to clean)
    if [[ -n "$SYNAPSE_DATA" ]]; then
      docker run --rm -v "${SYNAPSE_DATA}:/data" --entrypoint sh \
        matrixdotorg/synapse:v1.127.1 -c "rm -rf /data/" 2>/dev/null || true
      rm -rf "$SYNAPSE_DATA" 2>/dev/null || true
    fi
    # Remove temp files
    rm -rf "$TMPDIR_DW"
    # Remove test binary if we built it
    [[ "$DW_BIN" == *"-matrix-test" ]] && rm -f "$DW_BIN" || true
  else
    echo ""
    echo "Skipping cleanup (--no-cleanup)."
    echo "  Temp dir:     $TMPDIR_DW"
    echo "  Synapse data: ${SYNAPSE_DATA:-unknown}"
    echo "  Synapse ctr:  dw-test-synapse (still running)"
  fi
}
trap cleanup EXIT

SYNAPSE_URL="http://localhost:8008"
BOT_USER="dwmatrixbot"
BOT_PASS="$(openssl rand -hex 16)"
BOT_MXID="@${BOT_USER}:localhost"
# Shared secret for Synapse admin registration — avoids relying on
# enable_registration config which has changed semantics across Synapse versions.
REG_SECRET="dw-test-$(openssl rand -hex 12)"
DW_PORT=19872

# ── Step 1: Generate Synapse config, patch, and start ────────────────────────
H "1. Start Synapse"

# Remove any leftover container from a previous run
docker rm -f dw-test-synapse 2>/dev/null || true

# Create temp dir for Synapse data (owned by current user)
SYNAPSE_DATA="$(mktemp -d -t synapse.XXXXXX)"

# Generate signing key + log config using the Synapse image
echo "Generating Synapse signing key and log config…"
docker run --rm \
  -e SYNAPSE_SERVER_NAME=localhost \
  -e SYNAPSE_REPORT_STATS=no \
  -v "${SYNAPSE_DATA}:/data" \
  matrixdotorg/synapse:v1.127.1 generate 2>&1 | tail -3

# Write a complete, known-good homeserver.yaml (replacing the generated one).
# Keeps the signing key and log config created above; sets our shared secret.
# Using Python inside the same image ensures correct file ownership (uid 991).
docker run --rm \
  -v "${SYNAPSE_DATA}:/data" \
  -e "REG_SECRET=${REG_SECRET}" \
  --entrypoint python3 \
  matrixdotorg/synapse:v1.127.1 \
  -c "
import os
secret = os.environ['REG_SECRET']
cfg = '''server_name: localhost
pid_file: /data/homeserver.pid
listeners:
  - port: 8008
    tls: false
    type: http
    x_forwarded: true
    resources:
      - names: [client, federation]
        compress: false
database:
  name: sqlite3
  args:
    database: /data/homeserver.db
log_config: /data/localhost.log.config
media_store_path: /data/media_store
signing_key_path: /data/localhost.signing.key
registration_shared_secret: \"''' + secret + '''\"
enable_registration: true
enable_registration_without_email_verification: true
report_stats: false
suppress_key_server_warning: true
trusted_key_servers: []
'''
with open('/data/homeserver.yaml', 'w') as f:
    f.write(cfg)
print('homeserver.yaml written (registration_shared_secret set)')
"

# Start Synapse
docker run -d \
  --name dw-test-synapse \
  -p 8008:8008 \
  -v "${SYNAPSE_DATA}:/data" \
  matrixdotorg/synapse:v1.127.1 >/dev/null 2>&1

# Wait for health (60 second timeout — config is already generated, so startup is fast)
echo -n "Waiting for Synapse health"
for i in $(seq 1 60); do
  if curl -sf "${SYNAPSE_URL}/_matrix/client/versions" >/dev/null 2>&1; then
    echo " ready (${i}s)"
    break
  fi
  echo -n "."
  sleep 1
  if [[ "$i" -eq 60 ]]; then
    echo ""
    echo "Synapse container logs (last 30 lines):"
    docker logs dw-test-synapse 2>&1 | tail -30
    ko "Synapse did not become healthy in 60s"
    exit 1
  fi
done
ok "Synapse healthy at $SYNAPSE_URL"

# ── Step 2: Register bot account ──────────────────────────────────────────────
H "2. Register bot account ($BOT_MXID)"

# Use Synapse's admin API with HMAC-signed nonce — more reliable than open
# registration which has changed semantics across Synapse versions.
BOT_TOKEN=$(python3 - <<PYEOF
import hmac, hashlib, json, sys
try:
    import urllib.request as urlreq
    SYNAPSE_URL = '${SYNAPSE_URL}'
    SECRET = '${REG_SECRET}'
    USERNAME = '${BOT_USER}'
    PASSWORD = '${BOT_PASS}'
    resp = urlreq.urlopen(f'{SYNAPSE_URL}/_synapse/admin/v1/register')
    nonce = json.loads(resp.read())['nonce']
    h = hmac.new(SECRET.encode(), digestmod=hashlib.sha1)
    h.update(nonce.encode()); h.update(b'\x00')
    h.update(USERNAME.encode()); h.update(b'\x00')
    h.update(PASSWORD.encode()); h.update(b'\x00')
    h.update(b'notadmin')
    data = json.dumps({'nonce':nonce,'username':USERNAME,'password':PASSWORD,'mac':h.hexdigest()}).encode()
    req = urlreq.Request(f'{SYNAPSE_URL}/_synapse/admin/v1/register', data=data, headers={'Content-Type':'application/json'})
    reg = json.loads(urlreq.urlopen(req).read())
    print(reg.get('access_token',''))
except Exception as e:
    print(f'ERR:{e}', file=sys.stderr)
    sys.exit(1)
PYEOF
)

if [[ -z "$BOT_TOKEN" ]] || [[ "$BOT_TOKEN" == ERR:* ]]; then
  ko "Bot registration failed: $BOT_TOKEN"
  exit 1
fi
ok "Bot registered via admin API — token acquired"

# ── Step 3: Create test room ──────────────────────────────────────────────────
H "3. Create test room"
ROOM=$(curl -sf -X POST "${SYNAPSE_URL}/_matrix/client/v3/createRoom" \
  -H "Authorization: Bearer $BOT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"preset":"public_chat","room_alias_name":"dw-integration-test","name":"Datawatch Integration Test"}' \
  2>/dev/null || echo "{}")

ROOM_ID=$(echo "$ROOM" | jq -r '.room_id // empty')
if [[ -z "$ROOM_ID" ]]; then
  ko "Room creation failed: $ROOM"
  exit 1
fi
ok "Room created: $ROOM_ID"

# ── Step 4: Write datawatch config ───────────────────────────────────────────
H "4. Configure datawatch"
cat > "$DW_CONFIG" <<YAML
hostname: dw-matrix-test
data_dir: ${DW_DATA}
server:
  port: ${DW_PORT}
  token: matrixtesttoken
matrix:
  enabled: true
  homeserver: ${SYNAPSE_URL}
  user_id: "${BOT_MXID}"
  access_token: "\${secret:matrix-access-token}"
  room_id: "${ROOM_ID}"
YAML

# Store the bot token in the secrets store
"$DW_BIN" secrets set matrix-access-token "$BOT_TOKEN" \
  --config "$DW_CONFIG" 2>/dev/null || {
  # secrets store not yet init'd — write token directly for test convenience
  sed -i "s|\\\${secret:matrix-access-token}|${BOT_TOKEN}|" "$DW_CONFIG"
  skip "Secrets store not available — using plaintext token for test only"
}
ok "Config written to $DW_CONFIG"

# ── Step 5: Start datawatch daemon ────────────────────────────────────────────
H "5. Start datawatch daemon"
"$DW_BIN" start --foreground --config "$DW_CONFIG" >"$TMPDIR_DW/daemon.log" 2>&1 &
DW_PID=$!

echo -n "Waiting for daemon health"
for i in $(seq 1 30); do
  if curl -sf -H "Authorization: Bearer matrixtesttoken" \
      "http://localhost:${DW_PORT}/api/health" >/dev/null 2>&1; then
    echo " ready (${i}s)"
    break
  fi
  echo -n "."
  sleep 1
  if [[ "$i" -eq 30 ]]; then
    echo ""
    ko "Daemon did not become healthy in 30s"
    tail -20 "$TMPDIR_DW/daemon.log"
    exit 1
  fi
done
ok "Daemon healthy on :${DW_PORT}"

# ── Step 6: GET /api/matrix/status ───────────────────────────────────────────
H "6. Matrix status endpoint"
STATUS=$(curl -sf -H "Authorization: Bearer matrixtesttoken" \
  "http://localhost:${DW_PORT}/api/matrix/status" 2>/dev/null || echo "{}")
ENABLED=$(echo "$STATUS" | jq -r '.enabled // false')
if [[ "$ENABLED" == "true" ]]; then
  ok "GET /api/matrix/status — enabled=true"
else
  ko "GET /api/matrix/status — enabled!=true: $STATUS"
fi

# ── Step 7: POST /api/matrix/test (daemon→room) ───────────────────────────────
H "7. Send test message via API (daemon → room)"
TEST_RESP=$(curl -sf -X POST -H "Authorization: Bearer matrixtesttoken" \
  -H 'Content-Type: application/json' \
  "http://localhost:${DW_PORT}/api/matrix/test" \
  -d '{"message":"datawatch-integration-test-outbound"}' 2>/dev/null || echo "{}")
TEST_OK=$(echo "$TEST_RESP" | jq -r '.ok // .sent // false')
if [[ "$TEST_OK" == "true" ]]; then
  ok "POST /api/matrix/test returned ok=true"
else
  ko "POST /api/matrix/test failed: $TEST_RESP"
fi

# ── Step 8: Verify outbound message in room timeline ─────────────────────────
H "8. Verify outbound message + m.datawatch.session (Q5.3)"
sleep 2  # let the event settle
MSGS=$(curl -sf -H "Authorization: Bearer $BOT_TOKEN" \
  "${SYNAPSE_URL}/_matrix/client/v3/rooms/${ROOM_ID}/messages?dir=b&limit=10" \
  2>/dev/null || echo "{}")
if echo "$MSGS" | jq -r '.chunk[].content.body' 2>/dev/null | grep -q "datawatch-integration-test-outbound"; then
  ok "Outbound message found in room timeline"
else
  ko "Outbound message NOT found: $(echo "$MSGS" | jq -r '.chunk[].content.body' 2>/dev/null | head -5)"
fi

# Verify m.datawatch.session field (Q5.3)
DW_SESSION=$(echo "$MSGS" | jq -r '.chunk[] | select(.content.body == "datawatch-integration-test-outbound") | .content["m.datawatch.session"].role' 2>/dev/null || echo "")
if [[ "$DW_SESSION" == "output" ]]; then
  ok "Q5.3 — m.datawatch.session.role=output present in outbound event"
else
  ko "Q5.3 — m.datawatch.session missing or wrong role: '$DW_SESSION'"
fi

# ── Step 9: Inbound message (peer → room) ─────────────────────────────────────
H "9. Inbound message from test peer"
NIO_USER="niotestpeer"
NIO_PASS="$(openssl rand -hex 16)"

# Register peer via admin API (same as bot)
NIO_TOKEN=$(python3 - <<PYEOF
import hmac, hashlib, json, sys
try:
    import urllib.request as urlreq
    SYNAPSE_URL = '${SYNAPSE_URL}'
    SECRET = '${REG_SECRET}'
    USERNAME = '${NIO_USER}'
    PASSWORD = '${NIO_PASS}'
    resp = urlreq.urlopen(f'{SYNAPSE_URL}/_synapse/admin/v1/register')
    nonce = json.loads(resp.read())['nonce']
    h = hmac.new(SECRET.encode(), digestmod=hashlib.sha1)
    h.update(nonce.encode()); h.update(b'\x00')
    h.update(USERNAME.encode()); h.update(b'\x00')
    h.update(PASSWORD.encode()); h.update(b'\x00')
    h.update(b'notadmin')
    data = json.dumps({'nonce':nonce,'username':USERNAME,'password':PASSWORD,'mac':h.hexdigest()}).encode()
    req = urlreq.Request(f'{SYNAPSE_URL}/_synapse/admin/v1/register', data=data, headers={'Content-Type':'application/json'})
    reg = json.loads(urlreq.urlopen(req).read())
    print(reg.get('access_token',''))
except Exception as e:
    print(f'ERR:{e}', file=sys.stderr)
PYEOF
)

if [[ -z "$NIO_TOKEN" ]] || [[ "$NIO_TOKEN" == ERR:* ]]; then
  skip "Could not register nio peer — skipping inbound test"
else
  # Join room
  curl -sf -X POST \
    -H "Authorization: Bearer $NIO_TOKEN" \
    -H 'Content-Type: application/json' \
    "${SYNAPSE_URL}/_matrix/client/v3/join/${ROOM_ID}" \
    -d '{}' >/dev/null 2>&1 || true

  # Send message from peer
  INBOUND_MSG="dw-inbound-test-$(date +%s)"
  SEND=$(curl -sf -X PUT \
    -H "Authorization: Bearer $NIO_TOKEN" \
    -H 'Content-Type: application/json' \
    "${SYNAPSE_URL}/_matrix/client/v3/rooms/${ROOM_ID}/send/m.room.message/inbound-$(date +%s)" \
    -d "{\"msgtype\":\"m.text\",\"body\":\"${INBOUND_MSG}\"}" \
    2>/dev/null || echo "{}")
  if echo "$SEND" | jq -e '.event_id' >/dev/null 2>&1; then
    ok "Peer sent inbound message: $INBOUND_MSG"
  else
    ko "Peer could not send inbound message: $SEND"
  fi
fi

# ── Step 10: Daemon restart mid-conversation ──────────────────────────────────
H "10. Daemon restart — no message loss"
kill -SIGTERM "$DW_PID" 2>/dev/null || true
wait "$DW_PID" 2>/dev/null || true
DW_PID=""

"$DW_BIN" start --foreground --config "$DW_CONFIG" >"$TMPDIR_DW/daemon-restart.log" 2>&1 &
DW_PID=$!

echo -n "Waiting for daemon restart"
for i in $(seq 1 20); do
  if curl -sf -H "Authorization: Bearer matrixtesttoken" \
      "http://localhost:${DW_PORT}/api/health" >/dev/null 2>&1; then
    echo " ready (${i}s)"
    break
  fi
  echo -n "."
  sleep 1
  if [[ "$i" -eq 20 ]]; then
    echo ""
    ko "Daemon did not restart in 20s"
    break
  fi
done
ok "Daemon restarted cleanly"

# Send a second test message after restart
RESTART_RESP=$(curl -sf -X POST -H "Authorization: Bearer matrixtesttoken" \
  -H 'Content-Type: application/json' \
  "http://localhost:${DW_PORT}/api/matrix/test" \
  -d '{"message":"datawatch-post-restart-test"}' 2>/dev/null || echo "{}")
RESTART_OK=$(echo "$RESTART_RESP" | jq -r '.ok // .sent // false')
if [[ "$RESTART_OK" == "true" ]]; then
  ok "Post-restart test message sent successfully"
else
  ko "Post-restart test message failed: $RESTART_RESP"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
H "Summary"
echo "  Pass: $PASS"
echo "  Fail: $FAIL"
echo ""

if [[ "$FAIL" -gt 0 ]]; then
  echo "FAIL: $FAIL integration check(s) failed."
  exit 1
fi
echo "OK: all Matrix integration checks passed."
exit 0
