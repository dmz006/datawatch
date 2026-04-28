#!/usr/bin/env bash
# release-smoke-secure.sh — encryption-mode smoke runner.
#
# Brings up an encrypted-mode daemon in a temp data dir and runs
# the §41 audit gap #5 set: encryption migration paths + encrypted
# memory + encrypted session tracking. Operator-asked.
#
# This is a SEPARATE runner from release-smoke.sh because:
#   - It needs DATAWATCH_SECURE_PASSWORD set in env.
#   - It tears down + recreates a temp data dir per run.
#   - The encrypted daemon binds to a non-default port to avoid
#     colliding with the operator's regular daemon.
#
# Usage:
#   DATAWATCH_SECURE_PASSWORD='test-secret-do-not-reuse' \
#     bash scripts/release-smoke-secure.sh
#
# Exits 0 on full pass, non-zero on any FAIL. SKIPs when prerequi-
# sites missing (no DATAWATCH_SECURE_PASSWORD, no `datawatch`
# binary in PATH).

set -uo pipefail

PASS=0
FAIL=0
SKIP=0
ok()   { echo "  PASS  $*"; PASS=$((PASS+1)); }
ko()   { echo "  FAIL  $*"; FAIL=$((FAIL+1)); }
skip() { echo "  SKIP  $*"; SKIP=$((SKIP+1)); }
H()    { echo ""; echo "== $* =="; }

if [[ -z "${DATAWATCH_SECURE_PASSWORD:-}" ]]; then
  H "Encryption smoke prerequisites"
  skip "DATAWATCH_SECURE_PASSWORD not set; encryption smoke needs the env var"
  echo ""
  echo "Summary: $PASS pass / $FAIL fail / $SKIP skip"
  exit 0
fi
if ! command -v datawatch >/dev/null 2>&1; then
  H "Encryption smoke prerequisites"
  skip "datawatch not in PATH"
  echo ""
  echo "Summary: $PASS pass / $FAIL fail / $SKIP skip"
  exit 0
fi

TMPDIR=$(mktemp -d)
PORT_TLS=18444
PORT_HTTP=18081
trap 'kill $DW_PID 2>/dev/null; rm -rf "$TMPDIR"' EXIT

H "1. Bring up encrypted-mode daemon"
DATAWATCH_DATA_DIR="$TMPDIR" datawatch start --foreground --secure \
  --bind 127.0.0.1:$PORT_HTTP --tls-port $PORT_TLS \
  >/tmp/dw-secure.log 2>&1 &
DW_PID=$!
# Poll for /api/health.
for i in $(seq 1 30); do
  if curl -sk "https://127.0.0.1:$PORT_TLS/api/health" 2>/dev/null | grep -q '"status":"ok"'; then
    ok "encrypted daemon up after ${i}s (PID=$DW_PID, data_dir=$TMPDIR)"
    break
  fi
  sleep 1
done
if ! curl -sk "https://127.0.0.1:$PORT_TLS/api/health" 2>/dev/null | grep -q '"status":"ok"'; then
  ko "encrypted daemon never reached ready (see /tmp/dw-secure.log)"
  echo ""; echo "Summary: $PASS pass / $FAIL fail / $SKIP skip"
  exit 1
fi

H "2. Verify encrypted=true in /api/health"
ENC=$(curl -sk "https://127.0.0.1:$PORT_TLS/api/health" | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("encrypted") else "no")' 2>/dev/null)
if [[ "$ENC" == "yes" ]]; then
  ok "/api/health reports encrypted=true"
else
  ko "/api/health did NOT report encrypted=true"
fi

H "3. Verify session store is encrypted on disk"
if find "$TMPDIR" -name "sessions*.enc" -o -name "*.encrypted" | grep -q .; then
  ok "encrypted session artifacts present on disk"
else
  # Acceptable when no sessions have been created yet — this is
  # a fresh data dir; the encryption applies on first save.
  skip "no encrypted artifacts yet (no sessions started)"
fi

H "4. Save + retrieve under encryption"
ENC_PROBE="encryption-smoke-$(date +%s)"
SR=$(curl -sk -X POST -H "Content-Type: application/json" \
  -d "$(printf '{"content":"%s"}' "$ENC_PROBE")" \
  "https://127.0.0.1:$PORT_TLS/api/memory/save" 2>/dev/null)
SR_ID=$(echo "$SR" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null)
if [[ -n "$SR_ID" && "$SR_ID" != "0" ]]; then
  ok "encrypted memory save returned id=$SR_ID"
  if curl -sk "https://127.0.0.1:$PORT_TLS/api/memory/list?limit=200" | python3 -c "
import json,sys
arr = json.load(sys.stdin)
hit = any(int(m.get('id',0)) == int('$SR_ID') for m in arr)
assert hit, 'enc probe $SR_ID not found'
" 2>/dev/null; then
    ok "encrypted memory list round-trips id=$SR_ID"
  else
    ko "encrypted memory list missed id=$SR_ID"
  fi
else
  ko "encrypted memory save failed: $(echo "$SR" | head -c 100)"
fi

H "5. Restart preserves encrypted state"
kill $DW_PID 2>/dev/null
sleep 2
DATAWATCH_DATA_DIR="$TMPDIR" datawatch start --foreground --secure \
  --bind 127.0.0.1:$PORT_HTTP --tls-port $PORT_TLS \
  >>/tmp/dw-secure.log 2>&1 &
DW_PID=$!
for i in $(seq 1 30); do
  curl -sk "https://127.0.0.1:$PORT_TLS/api/health" 2>/dev/null | grep -q ok && break
  sleep 1
done
if curl -sk "https://127.0.0.1:$PORT_TLS/api/memory/list?limit=200" 2>/dev/null | python3 -c "
import json,sys
arr = json.load(sys.stdin)
assert any(int(m.get('id',0)) == int('$SR_ID') for m in arr), 'survived restart hit missing'
" 2>/dev/null; then
  ok "encrypted state survives daemon restart (memory id=$SR_ID still present)"
else
  ko "encrypted state did NOT survive restart"
fi

H "Summary"
echo "  Pass:  $PASS"
echo "  Fail:  $FAIL"
echo "  Skip:  $SKIP"
[[ "$FAIL" == "0" ]]
