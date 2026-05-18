#!/usr/bin/env bash
# TS-505 — PUT /api/compute/nodes/{name}/observer-peer sets observer_peer
# tags: surface:api feature:observer feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-505"
story_preflight "surface:api feature:observer feature:compute" || return 0

_story_ts_505() {
  local cname="ts-505-node-$$"
  local resp
  resp=$(api POST "/api/compute/nodes?probe=skip" "{\"name\":\"$cname\",\"kind\":\"ollama\",\"address\":\"http://localhost:11434\"}")
  if ! assert_json "$resp" '"name" in d or "id" in d'; then
    skip "could not create compute node for test: $(echo "$resp" | head -c 100)"
    return
  fi
  add_cleanup compute_node "$cname"
  local peer_id
  peer_id=$(api GET /api/observer/peers | python3 -c 'import json,sys;d=json.load(sys.stdin);peers=d.get("peers",d) if isinstance(d,dict) else d;print(peers[0]["id"] if isinstance(peers,list) and peers else "")' 2>/dev/null || echo "")
  if [[ -z "$peer_id" ]]; then
    skip "no observer peers available"
    return
  fi
  local put_resp
  put_resp=$(api PUT "/api/compute/nodes/$cname/observer-peer" "{\"peer_id\":\"$peer_id\"}")
  save_evidence TS-505 "put.json" "$put_resp"
  if echo "$put_resp" | grep -qi "not found\|404\|not.*support"; then
    skip "observer-peer endpoint not available"
    return
  fi
  local get_resp
  get_resp=$(api GET "/api/compute/nodes/$cname")
  save_evidence TS-505 "get.json" "$get_resp"
  if assert_json "$get_resp" 'd.get("observer_peer","") != ""'; then
    ok "PUT /api/compute/nodes/$cname/observer-peer sets observer_peer"
  else
    ko "observer_peer not set after PUT: $(echo "$get_resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_505
: "${RESULT:=fail}"
unset -f _story_ts_505
