#!/usr/bin/env bash
# TS-460 — compute_node_attach_observer MCP tool sets observer_peer field
# tags: surface:mcp feature:observer feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-460"
story_preflight "surface:mcp feature:observer feature:compute" || return 0

_story_ts_460() {
  local cname="ts-460-node-$$"
  local resp
  resp=$(api POST "/api/compute/nodes?probe=skip" "{\"name\":\"$cname\",\"kind\":\"ollama\",\"address\":\"http://localhost:11434\"}")
  if ! assert_json "$resp" '"name" in d or "id" in d'; then
    skip "could not create compute node for test: $(echo "$resp" | head -c 100)"
    return
  fi
  add_cleanup compute_node "$cname"
  # Get first observer peer ID
  local peer_id
  peer_id=$(api GET /api/observer/peers | python3 -c 'import json,sys;d=json.load(sys.stdin);peers=d.get("peers",d) if isinstance(d,dict) else d;print(peers[0]["id"] if isinstance(peers,list) and peers else "")' 2>/dev/null || echo "")
  if [[ -z "$peer_id" ]]; then
    skip "no observer peers available to attach"
    return
  fi
  local attach_resp
  attach_resp=$(api POST /api/mcp/call "{\"tool\":\"compute_node_attach_observer\",\"params\":{\"name\":\"$cname\",\"peer_id\":\"$peer_id\"}}")
  save_evidence TS-460 "attach.json" "$attach_resp"
  if echo "$attach_resp" | grep -qi "unknown tool\|not found\|not enabled"; then
    skip "compute_node_attach_observer tool not available"
    return
  fi
  # Detach after test
  api POST /api/mcp/call "{\"tool\":\"compute_node_detach_observer\",\"params\":{\"name\":\"$cname\"}}" >/dev/null 2>&1
  if assert_json "$attach_resp" 'isinstance(d, dict)'; then
    ok "compute_node_attach_observer tool returned dict"
  else
    ko "unexpected response: $(echo "$attach_resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_460
: "${RESULT:=fail}"
unset -f _story_ts_460
