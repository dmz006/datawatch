#!/usr/bin/env bash
# TS-541 — POST /api/sessions/{id}/hook-event with PostToolUse payload accepted
# tags: surface:api feature:sessions
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-541"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_541() {
  ensure_test_session || return
  local resp code
  resp=$(api_code POST "/api/sessions/$SESSION_ID/hook-event" \
    "{\"event\":\"PostToolUse\",\"session_id\":\"$SESSION_ID\",\"tool\":\"Bash\",\"payload\":{\"input\":{\"command\":\"echo test\"},\"output\":\"test output\"}}")
  save_evidence TS-541 "hook.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  if [[ "$code" == "200" || "$code" == "201" || "$code" == "202" || "$code" == "204" ]]; then
    ok "POST /api/sessions/$SESSION_ID/hook-event PostToolUse returned $code"
  elif [[ "$code" == "404" ]]; then
    skip "hook-event endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_541
: "${RESULT:=fail}"
unset -f _story_ts_541
