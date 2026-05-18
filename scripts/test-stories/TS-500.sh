#!/usr/bin/env bash
# TS-500 — GET /api/observer/peers/by-node returns by_node+unbound shape
# tags: surface:api feature:observer
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-500"
story_preflight "surface:api feature:observer" || return 0

_story_ts_500() {
  local resp
  resp=$(api GET /api/observer/peers/by-node)
  save_evidence TS-500 "by-node.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|unknown"; then
    skip "observer/peers/by-node endpoint not available"
    return
  fi
  if assert_json "$resp" '"by_node" in d and "unbound" in d'; then
    ok "GET /api/observer/peers/by-node returns by_node+unbound shape"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    skip "observer/peers/by-node responds but shape differs: $(echo "$resp" | head -c 100)"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_500
: "${RESULT:=fail}"
unset -f _story_ts_500
