#!/usr/bin/env bash
set -u
export PATH="/usr/bin:/bin:/usr/local/bin:$PATH"
TMP=$(cat /tmp/verify_tmp_path)
# parse ports from config
SSEPORT=$(grep 'sse_port:' "$TMP/config.yaml" | awk '{print $2}')
WEBPORT=$(grep -E '^  port:' "$TMP/config.yaml" | awk '{print $2}')
echo "holder on $SSEPORT, daemon web on $WEBPORT"
"$TMP/portholder" "127.0.0.1:$SSEPORT" > "$TMP/holder.log" 2>&1 &
HOLDER_PID=$!
sleep 1
if ! kill -0 "$HOLDER_PID" 2>/dev/null; then echo "HOLDER FAILED EARLY"; cat "$TMP/holder.log"; exit 1; fi
"$TMP/dw" start --config "$TMP/config.yaml" --foreground > "$TMP/daemon.log" 2>&1 &
DW_PID=$!
echo "$DW_PID" > "$TMP/dw.pid"
TOKEN=$(grep 'token:' "$TMP/config.yaml" | awk '{print $2}' | head -1)
health() {
  curl -s --max-time 2 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$WEBPORT/api/health" 2>/dev/null
}
echo "== phase 1: expect retrying (~5s window)"
PHASE1=""
for i in $(seq 1 18); do
  H=$(health)
  if [ -n "$H" ]; then
    V=$(echo "$H" | grep -o '"mcp_sse":"[^"]*"' || true)
    HTTP_OK=1
    echo "poll $i: $V"
    if echo "$V" | grep -q retrying; then PHASE1=retrying; fi
    if echo "$V" | grep -q '"listening"'; then PHASE1=early-listening; fi
  else
    echo "poll $i: no response"
  fi
  sleep 0.8
done
echo "PHASE1_RESULT=$PHASE1"
echo "== phase 2: expect listening within 30s"
PHASE2=""
for i in $(seq 1 40); do
  H=$(health)
  if [ -n "$H" ]; then
    V=$(echo "$H" | grep -o '"mcp_sse":"[^"]*"' || true)
    if echo "$V" | grep -q '"listening"'; then PHASE2=listening; echo "poll $i: $V"; break; fi
    if echo "$V" | grep -q '"failed"'; then echo "poll $i: $V (FAILED STATE)"; break; fi
  fi
  sleep 0.75
done
echo "PHASE2_RESULT=$PHASE2"
echo "== daemon liveness (still serving)"
H=$(health); echo "$H" | head -c 400; echo
echo "== SSE endpoint check"
curl -s --max-time 2 -i -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$SSEPORT/" | head -12
echo
echo "== daemon still running: $(kill -0 $DW_PID 2>/dev/null && echo yes || echo NO)"
echo SMOKE_DONE PID=$DW_PID
