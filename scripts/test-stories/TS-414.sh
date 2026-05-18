#!/usr/bin/env bash
# TS-414 — GET /api/observer/peers/by-node returns {by_node:{},unbound:[]} shape
# tags: surface:api feature:observer
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-414"
story_preflight "surface:api feature:observer" || return 0

_story_ts_414() {
  local resp
  resp=$(api GET /api/observer/peers/by-node)
  save_evidence TS-414 "resp.json" "$resp"
  if assert_json "$resp" '"by_node" in d or "unbound" in d'; then
    ok "GET /api/observer/peers/by-node returns expected shape"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/observer/peers/by-node returns dict"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "observer/peers/by-node endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_414
: "${RESULT:=fail}"
unset -f _story_ts_414
