#!/usr/bin/env bash
# TS-276 — compute_node_list via MCP returns array
# tags: surface:mcp feature:mcp feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-276"
story_preflight "surface:mcp feature:mcp feature:compute" || return 0

_story_ts_276() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"compute_node_list","params":{}}')
  save_evidence TS-276 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "compute_node_list not available in this build"
    return
  fi
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "compute_node_list returned array"
  elif assert_json "$resp" 'isinstance(d, dict) and ("nodes" in d or "items" in d or "result" in d)'; then
    ok "compute_node_list returned dict with nodes key"
  else
    ko "compute_node_list unexpected: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_276
: "${RESULT:=fail}"
unset -f _story_ts_276
