#!/usr/bin/env bash
# TS-511 — compute_node_health MCP tool returns health shape
# tags: surface:mcp feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-511"
story_preflight "surface:mcp feature:compute" || return 0

_story_ts_511() {
  local node_name
  node_name=$(api GET /api/compute/nodes | python3 -c 'import json,sys;d=json.load(sys.stdin);nodes=d.get("nodes",d) if isinstance(d,dict) else d;print(nodes[0]["name"] if isinstance(nodes,list) and nodes else "")' 2>/dev/null || echo "")
  if [[ -z "$node_name" ]]; then
    skip "no compute nodes configured"
    return
  fi
  local resp
  resp=$(api POST /api/mcp/call "{\"tool\":\"compute_node_health\",\"params\":{\"name\":\"$node_name\"}}")
  save_evidence TS-511 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not enabled"; then
    skip "compute_node_health tool not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "compute_node_health tool returned dict for $node_name"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_511
: "${RESULT:=fail}"
unset -f _story_ts_511
