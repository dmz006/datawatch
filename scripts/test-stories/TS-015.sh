#!/usr/bin/env bash
# TS-015 — Hook event: Stop
# tags: surface:api feature:sessions
# legacy fn: t2_ts015_hook_event_stop
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-015"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_015() {
  ensure_test_session || return
  local resp
  resp=$(api POST "/api/sessions/$SESSION_ID/hook-event" '{"event":"Stop","data":{"session_id":"'"$SESSION_ID"'"}}')
  save_evidence TS-015 "hook_stop.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "hook event Stop accepted"
  else
    ko "hook event Stop failed: $resp"
  fi
}

RESULT=fail
_story_ts_015
: "${RESULT:=fail}"
unset -f _story_ts_015
