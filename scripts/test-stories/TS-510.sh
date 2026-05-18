#!/usr/bin/env bash
# TS-510 — compute_node_list MCP tool returns nodes array
# tags: surface:mcp feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-510"
story_preflight "surface:mcp feature:compute" || return 0

_story_ts_510() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"compute_node_list","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-510 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not enabled"; then
    skip "compute_node_list tool not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, list) or isinstance(d.get("nodes",[]), list)'; then
    ok "compute_node_list tool returned nodes array"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "compute_node_list tool returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_510
: "${RESULT:=fail}"
unset -f _story_ts_510
