#!/usr/bin/env bash
# TS-017 — Channel history
# tags: surface:api feature:sessions
# legacy fn: t2_ts017_channel_history
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-017"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_017() {
  ensure_test_session || return
  local resp
  resp=$(curl "${curl_args[@]}" "$TEST_BASE/api/channel/history?session_id=$SESSION_ID")
  save_evidence TS-017 "channel_history.json" "$resp"
  if assert_json "$resp" '"messages" in d'; then
    ok "GET /api/channel/history returns messages key"
  else
    ko "channel history shape wrong: $resp"
  fi
}

RESULT=fail
_story_ts_017
: "${RESULT:=fail}"
unset -f _story_ts_017
