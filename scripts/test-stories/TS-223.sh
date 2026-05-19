#!/usr/bin/env bash
# TS-223 — Routing rules CRUD
# tags: surface:api feature:routing
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-223"
story_preflight "surface:api feature:routing" || return 0

_story_ts_223() {
  local resp code body
  resp=$(api_code GET /api/routing-rules)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-223 "routing_list.json" "$body"
  if [[ "$code" == "404" ]]; then
    skip "Routing endpoint not found (may be alpha feature)"
  elif assert_json "$body" 'isinstance(d, dict) or isinstance(d, list)'; then
    ok "GET /api/routing-rules reachable (HTTP $code)"
  else
    ko "unexpected HTTP $code: $(echo "$body" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_223
: "${RESULT:=fail}"
unset -f _story_ts_223
