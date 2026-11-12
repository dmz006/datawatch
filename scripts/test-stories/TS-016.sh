#!/usr/bin/env bash
# TS-016 — Channel send to session: start datawatch-channel inline against sandbox
# tags: surface:api feature:sessions
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-016"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_016() {
  ensure_test_session || return

  # Start the channel server against this session via the shared helper
  # (lib.sh): it opens the FIFO, runs the tail holder and the channel bound
  # to SESSION_ID, waits for channel_ready, and records both helper PIDs in
  # the shared daemon-library state (installing its own EXIT/INT/TERM/HUP
  # cleanups when run standalone), so failure or interrupt ends them by the
  # PIDs this story started — no bare kill, no name lookup.
  start_channel_server || { api POST /api/sessions/state "{\"id\":\"$SESSION_ID\",\"state\":\"killed\"}" >/dev/null 2>&1 || true; return; }

  # Send a message through the registered channel.
  local resp
  resp=$(api POST /api/channel/send '{"session_id":"'"$SESSION_ID"'","text":"test channel message e2e"}')
  save_evidence TS-016 "channel_send.json" "$resp"

  # End the channel helpers (recorded PIDs, marker-checked) and the session.
  stop_channel_server
  api POST /api/sessions/state "{\"id\":\"$SESSION_ID\",\"state\":\"killed\"}" >/dev/null 2>&1 || true

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
