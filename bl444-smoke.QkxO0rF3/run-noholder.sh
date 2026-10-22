#!/usr/bin/env bash
# BL444 SSE bind-retry live smoke. Everything confined to this temp dir.
set -u
ROOT=/home/dmz/.datawatch/prd-worktrees/10110001
S="$ROOT/bl444-smoke.QkxO0rF3"
SSE_PORT=39945
WEB_PORT=44089
TOKEN=bl444smoketoken
HOLD_S=12      # holder keeps the SSE port bound this long
FAIL=0

say() { printf '[smoke %s] %s\n' "$(date '+%H:%M:%S')" "$*"; }
health() {
  curl -s --max-time 2 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$WEB_PORT/api/health" 2>/dev/null || echo CURL_FAIL
}

# Clean slate for this attempt
rm -rf "$S/data" "$S/poll.txt" "$S/daemon.log" "$S/holder.log" "$S/sse-err.txt" "$S/stop.log"
: > "$S/poll.txt"

# Disable claude-channel wiring for this smoke (skips `claude mcp list`
# subprocesses during channel registration cleanup, which is host-contended
# and unrelated to BL444). Entry is not auto_created so no backfill flips it.
mkdir -p "$S/data/inference"
cat > "$S/data/inference/llms.json" <<'EOF'
[{"name":"claude-code","kind":"claude-code","disabled":false,"auto_created":false}]
EOF

# ---- 1. Holder: occupy the SSE port for ${HOLD_S}s, then release ----
# "$S/tools/holder" "$SSE_PORT" "${HOLD_S}s" > "$S/holder.log" 2>&1 &
HOLDER=""   # holder disabled in this test copy
HOLDER=$!
# sleep 0.3

# ---- 2. Start the daemon (foreground; session-end path = TERM in step 7) ----
"$S/dw" start --foreground --config "$ROOT/$S/config.yaml" > "$S/daemon.log" 2>&1 &
DPID=$!
say "holder_pid=$HOLDER daemon_pid=$DPID hold=${HOLD_S}s"

# ---- 3. Poll every second; cover daemon startup through the whole hold ----
PHASE1_END=$(( $(date +%s) + HOLD_S + 2 ))
served_ok=0; saw_retrying=0; saw_up_during_hold=0
while [ $(date +%s) -lt "$PHASE1_END" ]; do
  body=$(health)
  echo "$(date '+%H:%M:%S.%N') $body" >> "$S/poll.txt"
  case "$body" in
    *'"status":"ok"'*)
      served_ok=1
      # holder still running => port still held
      if [ -n "$HOLDER" ] && kill -0 "$HOLDER" 2>/dev/null; then saw_up_during_hold=1; fi
      ;;
  esac
  case "$body" in
    *'"mcp_sse":"retrying"'*) saw_retrying=1 ;;
    *'"mcp_sse":"listening"'*) break ;;
  esac
  sleep 1
done
[ "$served_ok" = 1 ]       && say "PASS: daemon served /api/health (200 JSON) — rest of daemon up" \
                           || { say "FAIL: no 200 from /api/health"; FAIL=1; }
[ "$saw_up_during_hold" = 1 ] && say "PASS: /api/health answered 200 while SSE port was still held" \
                               || say "INFO: no 200 sampled while holder still alive (timing)"
[ "$saw_retrying" = 1 ]    && say "PASS: mcp_sse=retrying observed" \
                            || { say "FAIL: mcp_sse=retrying never observed"; FAIL=1; }
if kill -0 "$DPID" 2>/dev/null; then say "PASS: daemon never crashed while binding was retried"; else say "FAIL: daemon process died"; FAIL=1; fi

# ---- 4. Wait for mcp_sse=listening after the holder frees the port ----
DEADLINE=$(( $(date +%s) + 30 ))
saw_listening=0
while [ $(date +%s) -lt "$DEADLINE" ]; do
  body=$(health)
  echo "$(date '+%H:%M:%S.%N') $body" >> "$S/poll.txt"
  case "$body" in *'"mcp_sse":"listening"'*) saw_listening=1; break ;; esac
  sleep 1
done
[ "$saw_listening" = 1 ] && say "PASS: mcp_sse=listening after port freed" || { say "FAIL: mcp_sse never became listening in 30s"; FAIL=1; }

# ---- 5. Log assertions ----
firstfail=$(grep -c 'MCP SSE: bind .* failed:' "$S/daemon.log" 2>/dev/null || true)
finalok=$(grep -c 'MCP SSE: all [0-9][0-9]* address(es) bound after' "$S/daemon.log" 2>/dev/null || true)
retrylines=$(grep -c 'still unbound.*retrying in' "$S/daemon.log" 2>/dev/null || true)
gaveup=$(grep -c 'MCP SSE: gave up after' "$S/daemon.log" 2>/dev/null || true)
[ "$firstfail" = 1 ] && say "PASS: exactly one info first-failure bind line" || { say "FAIL: first-failure bind lines = '$firstfail' (want 1)"; FAIL=1; }
[ "$finalok" = 1 ]   && say "PASS: exactly one final success line"       || { say "FAIL: success lines = '$finalok' (want 1)"; FAIL=1; }
[ "${retrylines:-0}" -ge 1 ] 2>/dev/null && say "PASS: backoff retry lines present ($retrylines)" || say "INFO: no explicit backoff retry lines seen"
[ "$gaveup" = 0 ]    && say "PASS: no give-up lines"                    || { say "FAIL: give-up line in log"; FAIL=1; }

# ---- 6. SSE endpoint answers (connection accepted, not refused) ----
sse_accepted=0
for path in /sse /; do
  code=$(curl --max-time 2 -sS -o "$S/sse-body" -w '%{http_code}' "http://127.0.0.1:$SSE_PORT$path" 2>"$S/sse-err.txt" || true)
  bodyhead=$(head -c 200 "$S/sse-body" 2>/dev/null | tr '\n' ' ')
  say "sse probe $path -> http_code=${code:-CURLErr} body=${bodyhead:-<empty>} err=$(head -1 "$S/sse-err.txt" 2>/dev/null)"
  case "$code" in
    ""|000) : ;;
    *) sse_accepted=1 ;;
  esac
done
[ "$sse_accepted" = 1 ] && say "PASS: SSE port answered (connection accepted)" || { say "FAIL: SSE endpoint refused connections"; FAIL=1; }

# ---- 7. Graceful stop via the normal session-end path (TERM to the daemon) ----
if kill -0 "$DPID" 2>/dev/null; then
  kill -TERM "$DPID"
  waited=0; exited=0
  while [ $waited -lt 30 ]; do
    kill -0 "$DPID" 2>/dev/null || { exited=1; break; }
    sleep 1; waited=$((waited+1))
  done
  if [ "$exited" = 1 ]; then
    wait "$DPID"; code=$?
    say "daemon exited within ${waited}s, exit code $code"
    case "$code" in
      0|130|143) say "PASS: clean shutdown via session-end path" ;;
      *) say "FAIL: unexpected exit code $code"; FAIL=1 ;;
    esac
  else
    say "FAIL: daemon did not exit within 30s of the session-end signal"
    FAIL=1
  fi
else
  say "FAIL: daemon already absent before stop step"; FAIL=1
fi
wait "$HOLDER" 2>/dev/null

# ---- 8. Ports free afterwards ----
for p in "$SSE_PORT" "$WEB_PORT"; do
  if (exec 3<>/dev/tcp/127.0.0.1/$p) 2>/dev/null; then
    say "FAIL: port $p still answering after shutdown"; FAIL=1
  else
    say "PASS: port $p free after shutdown"
  fi
done

echo
if [ "$FAIL" = 0 ]; then echo "SMOKE RESULT: ALL PASS"; else echo "SMOKE RESULT: FAILURES PRESENT"; fi
exit "$FAIL"
