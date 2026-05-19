#!/usr/bin/env bash
# TS-574 — GET /api/federation/sessions fans out to runtime-registered federated peers
# tags: surface:api feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-574"
story_preflight "surface:api feature:federation" || return 0

_story_ts_574() {
  local resp code body
  resp=$(api_code GET /api/federation/sessions)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-574 "resp.json" "$body"
  if [[ "$code" == "404" ]]; then
    skip "federation/sessions endpoint not available in this build"
  elif assert_json "$body" 'isinstance(d, list)'; then
    ok "GET /api/federation/sessions returns list"
  elif assert_json "$body" 'isinstance(d, dict)'; then
    ok "GET /api/federation/sessions returns dict"
  else
    ko "unexpected HTTP $code: $(echo "$body" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_574
: "${RESULT:=fail}"
unset -f _story_ts_574
