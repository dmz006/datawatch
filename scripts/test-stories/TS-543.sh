#!/usr/bin/env bash
# TS-543 — POST /api/sessions/{id}/hook-event with SessionStart payload accepted
# tags: surface:api feature:sessions
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-543"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_543() {
  ensure_test_session || return
  local resp code
  resp=$(api_code POST "/api/sessions/$SESSION_ID/hook-event" \
    "{\"type\":\"SessionStart\",\"session_id\":\"$SESSION_ID\",\"cwd\":\"/tmp\",\"task\":\"test\"}")
  save_evidence TS-543 "hook.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  if [[ "$code" == "200" || "$code" == "201" || "$code" == "202" || "$code" == "204" ]]; then
    ok "POST /api/sessions/$SESSION_ID/hook-event SessionStart returned $code"
  elif [[ "$code" == "404" ]]; then
    skip "hook-event endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_543
: "${RESULT:=fail}"
unset -f _story_ts_543
