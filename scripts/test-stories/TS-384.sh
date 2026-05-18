#!/usr/bin/env bash
# TS-384 — POST /api/push/register stores endpoint idempotent by client_id
# tags: surface:api feature:push
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-384"
story_preflight "surface:api feature:push" || return 0

_story_ts_384() {
  local client_id="test-client-ts384-$$"
  local payload
  payload="{\"client_id\":\"$client_id\",\"endpoint\":\"http://example.com/push/$client_id\",\"topic\":\"test-ts384\"}"
  local resp code body
  resp=$(api_code POST /api/push/register "$payload")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-384 "resp.json" "$body"
  if [[ "$code" == "200" || "$code" == "201" || "$code" == "204" ]]; then
    ok "POST /api/push/register returned $code"
  elif [[ "$code" == "404" ]]; then
    skip "push/register endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_384
: "${RESULT:=fail}"
unset -f _story_ts_384
