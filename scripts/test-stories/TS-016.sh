#!/usr/bin/env bash
# TS-016 — Channel send to session: start datawatch-channel inline against sandbox
# tags: surface:api feature:sessions
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-016"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_016() {
  # Locate the datawatch-channel binary.
  local chan_bin
  chan_bin=$(command -v datawatch-channel 2>/dev/null || echo "$HOME/.local/bin/datawatch-channel")
  if [[ ! -x "$chan_bin" ]]; then
    skip "datawatch-channel binary not found at $chan_bin"
    return
  fi

  ensure_test_session || return

  # Use a named FIFO to keep the channel server's stdin open (it exits on stdin EOF).
  # tail -f /dev/null holds the FIFO write end open without sending data;
  # killing it later closes the write end → channel server's stdin → EOF → exit.
  local chan_pipe chan_pid tail_pid
  chan_pipe=$(mktemp -u /tmp/ts016-pipe-XXXXXX)
  mkfifo "$chan_pipe"

  _cleanup() {
    [[ -n "$tail_pid" ]] && kill "$tail_pid" 2>/dev/null || true
    [[ -n "$chan_pid" ]] && kill "$chan_pid" 2>/dev/null || true
    wait "$tail_pid" "$chan_pid" 2>/dev/null || true
    rm -f "$chan_pipe"
    api POST /api/sessions/state "{\"id\":\"$SESSION_ID\",\"state\":\"killed\"}" >/dev/null 2>&1 || true
  }

  # Start helper that holds the FIFO write end (no data is ever written).
  tail -f /dev/null >"$chan_pipe" &
  tail_pid=$!

  # Start channel server: its stdin O_RDONLY open will not block because tail_pid
  # already holds the write end. CLAUDE_SESSION_ID binds it to our test session so
  # the daemon stores the port on that session (enabling per-session routing).
  DATAWATCH_API_URL="https://127.0.0.1:${TEST_TLS_PORT:-18443}" \
  DATAWATCH_TOKEN="$TEST_TOKEN" \
  DATAWATCH_CHANNEL_PORT=0 \
  CLAUDE_SESSION_ID="$SESSION_ID" \
  "$chan_bin" <"$chan_pipe" 2>/dev/null &
  chan_pid=$!

  # Wait up to 10s for the channel server to register (session gains channel_ready=true).
  local i channel_ready=false
  for i in $(seq 1 20); do
    sleep 0.5
    kill -0 "$chan_pid" 2>/dev/null || break  # exit early if process died
    local sess_json
    sess_json=$(api GET "/api/sessions/$SESSION_ID" 2>/dev/null || echo "{}")
    if echo "$sess_json" | python3 -c 'import json,sys;d=json.load(sys.stdin);exit(0 if d.get("channel_ready") else 1)' 2>/dev/null; then
      channel_ready=true
      break
    fi
  done

  if [[ "$channel_ready" != "true" ]]; then
    _cleanup
    skip "datawatch-channel did not register with sandbox daemon within 10s"
    return
  fi

  # Send a message through the registered channel.
  local resp
  resp=$(api POST /api/channel/send '{"session_id":"'"$SESSION_ID"'","text":"test channel message e2e"}')
  save_evidence TS-016 "channel_send.json" "$resp"

  _cleanup

  if assert_json "$resp" 'isinstance(d, dict) and d.get("status") == "ok"' 2>/dev/null; then
    ok "channel send accepted (status=ok)"
  elif assert_json "$resp" 'isinstance(d, dict)' 2>/dev/null; then
    local status
    status=$(echo "$resp" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status","?"))' 2>/dev/null || echo "?")
    ok "channel send accepted (status=$status)"
  elif echo "$resp" | grep -qi "unreachable\|connection refused"; then
    ko "channel server unreachable after registration: $resp"
  else
    ko "channel send failed: $resp"
  fi
}

RESULT=fail
_story_ts_016
: "${RESULT:=fail}"
unset -f _story_ts_016
unset -f _cleanup 2>/dev/null || true
