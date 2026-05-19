#!/usr/bin/env bash
# TS-225 — Observer peers surface
# tags: surface:api feature:peers
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-225"
story_preflight "surface:api feature:peers" || return 0

_story_ts_225() {
  local resp code body
  resp=$(api_code GET /api/observer/peers)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-225 "peers.json" "$body"
  if [[ "$code" == "404" ]]; then
    skip "Observer peers endpoint not found"
  elif assert_json "$body" '"peers" in d or isinstance(d, list)'; then
    ok "GET /api/observer/peers reachable (HTTP $code)"
  else
    ko "unexpected HTTP $code: $(echo "$body" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_225
: "${RESULT:=fail}"
unset -f _story_ts_225
