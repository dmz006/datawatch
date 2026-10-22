#!/usr/bin/env bash
set -u
TMP=$(cat /tmp/verify_tmp_path)
SSEPORT=$(grep 'sse_port:' "$TMP/config.yaml" | awk '{print $2}')
WEBPORT=$(grep -E '^  port:' "$TMP/config.yaml" | awk '{print $2}')
TOKEN=$(awk '/token:/{print $2; exit}' "$TMP/config.yaml")
echo "holder on $SSEPORT, daemon web on $WEBPORT"
"$TMP/portholder" "127.0.0.1:$SSEPORT" > "$TMP/holder.log" 2>&1 &
HOLDER_PID=$!
sleep 1
"$TMP/dw" start --config "$TMP/config.yaml" --foreground -v > "$TMP/daemon.log" 2>&1 &
DW_PID=$!
echo "$DW_PID" > "$TMP/dw.pid"
sleep 8
echo "=== alive? $(kill -0 $DW_PID 2>/dev/null && echo yes || echo NO)"
HEALTH=$(curl -s --max-time 2 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$WEBPORT/api/health" 2>/dev/null || echo NO_RESPONSE)
echo "HEALTH: $HEALTH"
echo "=== daemon.log so far ==="
cat "$TMP/daemon.log"
if kill -0 $DW_PID 2>/dev/null; then
  echo "KEEPING DAEMON ALIVE, pid=$DW_PID"
  echo "WAITING_TO_KILL"
  sleep 30
fi
echo "=== final log ==="
tail -40 "$TMP/daemon.log"
kill -0 $DW_PID 2>/dev/null && echo "STILL_ALIVE_AT_END" || echo "DIED"
