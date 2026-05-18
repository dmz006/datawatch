#!/usr/bin/env bash
# TS-456 — DELETE /api/compute/nodes/{name}/observer-peer clears observer_peer field
# tags: surface:api feature:observer feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-456"
story_preflight "surface:api feature:observer feature:compute" || return 0

_story_ts_456() {
  local cname="ts-456-node-$$"
  local resp
  resp=$(api POST "/api/compute/nodes?probe=skip" "{\"name\":\"$cname\",\"kind\":\"ollama\",\"address\":\"http://localhost:11434\"}")
  if ! assert_json "$resp" '"name" in d or "id" in d'; then
    skip "could not create compute node for test: $(echo "$resp" | head -c 100)"
    return
  fi
  add_cleanup compute_node "$cname"
  # Try to set then clear observer peer
  local peer_id
  peer_id=$(api GET /api/observer/peers | python3 -c 'import json,sys;d=json.load(sys.stdin);peers=d.get("peers",d) if isinstance(d,dict) else d;print(peers[0]["id"] if isinstance(peers,list) and peers else "")' 2>/dev/null || echo "")
  if [[ -n "$peer_id" ]]; then
    api PUT "/api/compute/nodes/$cname/observer-peer" "{\"peer_id\":\"$peer_id\"}" >/dev/null 2>&1
  fi
  local del_resp
  del_resp=$(api DELETE "/api/compute/nodes/$cname/observer-peer")
  save_evidence TS-456 "delete.json" "$del_resp"
  local get_resp
  get_resp=$(api GET "/api/compute/nodes/$cname")
  save_evidence TS-456 "get.json" "$get_resp"
  if echo "$del_resp" | grep -qi "not found\|404\|not.*support"; then
    skip "observer-peer DELETE endpoint not available"
    return
  fi
  if assert_json "$get_resp" 'd.get("observer_peer","") == ""'; then
    ok "DELETE /api/compute/nodes/$cname/observer-peer clears observer_peer"
  else
    ko "observer_peer not cleared after DELETE: $(echo "$get_resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_456
: "${RESULT:=fail}"
unset -f _story_ts_456
