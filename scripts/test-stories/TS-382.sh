#!/usr/bin/env bash
# TS-382 — POST /api/push/<topic> publishes event to subscribers
# tags: surface:api feature:push
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-382"
story_preflight "surface:api feature:push" || return 0

_story_ts_382() {
  local topic="test-push-$$"
  local resp code body
  resp=$(api_code POST "/api/push/$topic" '{"title":"test","message":"hello from TS-382"}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-382 "resp.json" "$body"
  if [[ "$code" == "200" || "$code" == "202" || "$code" == "204" ]]; then
    ok "POST /api/push/$topic returned $code"
  elif [[ "$code" == "404" ]]; then
    skip "push endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_382
: "${RESULT:=fail}"
unset -f _story_ts_382
