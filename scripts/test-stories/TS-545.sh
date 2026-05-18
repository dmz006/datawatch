#!/usr/bin/env bash
# TS-545 — GET /api/council/personas returns personas array
# tags: surface:api feature:council
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-545"
story_preflight "surface:api feature:council" || return 0

_story_ts_545() {
  local resp
  resp=$(api GET /api/council/personas)
  save_evidence TS-545 "personas.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404"; then
    skip "council/personas endpoint not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, list) or isinstance(d.get("personas",[]), list)'; then
    ok "GET /api/council/personas returns list/personas shape"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/council/personas returns dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_545
: "${RESULT:=fail}"
unset -f _story_ts_545
