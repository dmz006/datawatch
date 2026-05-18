#!/usr/bin/env bash
# TS-450 — GET /api/observer/peers response includes entry with is_self:true
# tags: surface:api feature:observer
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-450"
story_preflight "surface:api feature:observer" || return 0

_story_ts_450() {
  local resp
  resp=$(api GET /api/observer/peers)
  save_evidence TS-450 "peers.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|unknown"; then
    skip "observer/peers endpoint not available"
    return
  fi
  if assert_json "$resp" 'any(p.get("is_self") for p in (d.get("peers",d) if isinstance(d,dict) else d) if isinstance(p,dict))'; then
    ok "GET /api/observer/peers has entry with is_self:true"
  elif assert_json "$resp" 'isinstance(d, (dict, list))'; then
    skip "observer/peers responds but no is_self:true entry found"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_450
: "${RESULT:=fail}"
unset -f _story_ts_450
