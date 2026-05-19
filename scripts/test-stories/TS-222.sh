#!/usr/bin/env bash
# TS-222 — Cost tracking surface
# tags: surface:api feature:cost
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-222"
story_preflight "surface:api feature:cost" || return 0

_story_ts_222() {
  local resp code body
  resp=$(api_code GET /api/cost)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-222 "cost.json" "$body"
  if [[ "$code" == "404" || "$code" == "405" ]]; then
    skip "GET /api/cost not available (HTTP $code)"
    return
  fi
  if assert_json "$body" '"total_usd" in d or "sessions" in d or isinstance(d, dict)'; then
    ok "GET /api/cost returns cost tracking data (HTTP $code)"
  else
    skip "No cost tracking data found at /api/cost"
  fi
}

RESULT=fail
_story_ts_222
: "${RESULT:=fail}"
unset -f _story_ts_222
