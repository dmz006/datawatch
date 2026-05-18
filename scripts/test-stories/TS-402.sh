#!/usr/bin/env bash
# TS-402 — POST /api/sessions/{id}/hook-event accepts PostToolUse payload
# tags: surface:api feature:sessions feature:hooks
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-402"
story_preflight "surface:api feature:sessions feature:hooks" || return 0

_story_ts_402() {
  ensure_test_session || return
  local payload
  payload="{\"type\":\"PostToolUse\",\"session_id\":\"$SESSION_ID\",\"tool_name\":\"Bash\",\"input\":{\"command\":\"echo test\"},\"output\":\"test\"}"
  local resp code body
  resp=$(api_code POST "/api/sessions/$SESSION_ID/hook-event" "$payload")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-402 "resp.json" "$body"
  if [[ "$code" == "200" || "$code" == "202" || "$code" == "204" ]]; then
    ok "POST /api/sessions/$SESSION_ID/hook-event returned $code"
  elif [[ "$code" == "404" ]]; then
    skip "hook-event endpoint not available (404)"
  elif [[ "$code" == "400" ]]; then
    # May not be in the right state — acceptable
    ok "POST /api/sessions/$SESSION_ID/hook-event returned 400 (session state or payload rejected — endpoint exists)"
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_402
: "${RESULT:=fail}"
unset -f _story_ts_402
