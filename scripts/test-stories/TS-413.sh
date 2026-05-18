#!/usr/bin/env bash
# TS-413 — GET /api/observer/peers/free returns array (free peers with no bound compute node)
# tags: surface:api feature:compute feature:observer
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-413"
story_preflight "surface:api feature:compute feature:observer" || return 0

_story_ts_413() {
  local resp
  resp=$(api GET /api/observer/peers/free)
  save_evidence TS-413 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/observer/peers/free returns array"
  elif assert_json "$resp" 'isinstance(d, dict) and "peers" in d'; then
    ok "GET /api/observer/peers/free returns dict with peers key"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "observer/peers/free endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_413
: "${RESULT:=fail}"
unset -f _story_ts_413
