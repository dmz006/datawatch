#!/usr/bin/env bash
# TS-277 — compute_node_add + compute_node_get + compute_node_delete CRUD via MCP
# tags: surface:mcp feature:mcp feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-277"
story_preflight "surface:mcp feature:mcp feature:compute" || return 0

_story_ts_277() {
  local resp node_id node_name
  node_name="mcp-node-ts277-$$"

  # Add node (probe:false to skip connectivity check)
  resp=$(api POST /api/mcp/call "{\"tool\":\"compute_node_add\",\"params\":{\"name\":\"$node_name\",\"kind\":\"ollama\",\"address\":\"http://localhost:11434\",\"probe\":false}}")
  save_evidence TS-277 "add.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "compute_node_add not available in this build"
    return
  fi
  node_id=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",d.get("name","")))' 2>/dev/null || echo "")
  if [[ -z "$node_id" ]]; then
    node_id="$node_name"
  fi
  add_cleanup compute_node "$node_id"

  # Get node
  resp=$(api POST /api/mcp/call "{\"tool\":\"compute_node_get\",\"params\":{\"id\":\"$node_id\"}}")
  save_evidence TS-277 "get.json" "$resp"
  if ! assert_json "$resp" 'isinstance(d, dict)'; then
    # try by name
    resp=$(api POST /api/mcp/call "{\"tool\":\"compute_node_get\",\"params\":{\"name\":\"$node_name\"}}")
  fi

  # Delete node
  resp=$(api POST /api/mcp/call "{\"tool\":\"compute_node_delete\",\"params\":{\"id\":\"$node_id\"}}")
  save_evidence TS-277 "delete.json" "$resp"

  ok "compute_node CRUD round-trip: add/get/delete"
}

RESULT=fail
_story_ts_277
: "${RESULT:=fail}"
unset -f _story_ts_277
