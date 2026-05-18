#!/usr/bin/env bash
# TS-014 — Hook event: Activity
# tags: surface:api feature:sessions
# legacy fn: t2_ts014_hook_event_activity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-014"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_014() {
  ensure_test_session || return
  local resp
  resp=$(api POST "/api/sessions/$SESSION_ID/hook-event" '{"event":"Activity","data":{"session_id":"'"$SESSION_ID"'","text":"test activity"}}')
  save_evidence TS-014 "hook_activity.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "hook event Activity accepted"
  else
    ko "hook event Activity failed: $resp"
  fi
}

RESULT=fail
_story_ts_014
: "${RESULT:=fail}"
unset -f _story_ts_014
