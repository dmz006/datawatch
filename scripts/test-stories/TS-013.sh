#!/usr/bin/env bash
# TS-013 — Hook event: Start
# tags: surface:api feature:sessions
# legacy fn: t2_ts013_hook_event_start
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-013"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_013() {
  ensure_test_session || return
  local resp
  resp=$(api POST "/api/sessions/$SESSION_ID/hook-event" '{"event":"Start","data":{"session_id":"'"$SESSION_ID"'"}}')
  save_evidence TS-013 "hook_start.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "hook event Start accepted"
  else
    ko "hook event Start failed: $resp"
  fi
}

RESULT=fail
_story_ts_013
: "${RESULT:=fail}"
unset -f _story_ts_013
