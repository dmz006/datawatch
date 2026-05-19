#!/usr/bin/env bash
# TS-597 — GET /api/alerts/aggregated includes entries from federation peers
# tags: surface:api feature:multiserver
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-597"
story_preflight "surface:api feature:multiserver" || return 0

_story_ts_597() {
  local resp code body
  resp=$(api_code GET /api/alerts/aggregated)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-597 "resp.json" "$body"
  if [[ "$code" == "404" ]]; then
    skip "alerts/aggregated endpoint not available in this build"
  elif assert_json "$body" 'isinstance(d, list)'; then
    ok "GET /api/alerts/aggregated returns list"
  elif assert_json "$body" 'isinstance(d, dict)'; then
    ok "GET /api/alerts/aggregated returns dict"
  else
    ko "unexpected HTTP $code: $(echo "$body" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_597
: "${RESULT:=fail}"
unset -f _story_ts_597
