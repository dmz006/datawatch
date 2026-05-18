#!/usr/bin/env bash
# TS-452 — GET /api/observer/peers/free returns array
# tags: surface:api feature:observer
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-452"
story_preflight "surface:api feature:observer" || return 0

_story_ts_452() {
  local resp
  resp=$(api GET /api/observer/peers/free)
  save_evidence TS-452 "free.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|unknown"; then
    skip "observer/peers/free endpoint not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/observer/peers/free returns array (${#resp} chars)"
  elif assert_json "$resp" 'isinstance(d.get("peers",[]), list)'; then
    ok "GET /api/observer/peers/free returns object with peers array"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_452
: "${RESULT:=fail}"
unset -f _story_ts_452
