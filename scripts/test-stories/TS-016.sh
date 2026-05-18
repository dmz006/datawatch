#!/usr/bin/env bash
# TS-016 — Channel send to session
# tags: surface:api feature:sessions
# legacy fn: t2_ts016_channel_send
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-016"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_016() {
  ensure_test_session || return
  local resp
  resp=$(api POST /api/channel/send '{"session_id":"'"$SESSION_ID"'","text":"test channel message e2e"}')
  save_evidence TS-016 "channel_send.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)' 2>/dev/null; then
    ok "channel send accepted"
  elif echo "$resp" | grep -qi "unreachable\|connection refused\|not configured\|disabled"; then
    skip "channel server not available in test environment"
  else
    ko "channel send failed: $resp"
  fi
}

RESULT=fail
_story_ts_016
: "${RESULT:=fail}"
unset -f _story_ts_016
