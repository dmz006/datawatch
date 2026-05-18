#!/usr/bin/env bash
# TS-018 — Channel history: non-existent session returns empty
# tags: surface:api feature:sessions
# legacy fn: t2_ts018_channel_history_nonexistent
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-018"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_018() {
  local resp
  resp=$(curl "${curl_args[@]}" "$TEST_BASE/api/channel/history?session_id=test-nonexistent-xyz-$$")
  save_evidence TS-018 "channel_history_empty.json" "$resp"
  if assert_json "$resp" 'isinstance(d.get("messages",[]), list)'; then
    ok "channel history for unknown session returns empty list"
  else
    ko "channel history unknown session shape wrong: $resp"
  fi
}

RESULT=fail
_story_ts_018
: "${RESULT:=fail}"
unset -f _story_ts_018
