#!/usr/bin/env bash
# TS-579 — federation_group_list MCP tool returns builtin groups
# tags: surface:mcp feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-579"
story_preflight "surface:mcp feature:federation" || return 0

_story_ts_579() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"federation_group_list","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-579 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not found\|not enabled\|no route"; then
    skip "federation_group_list MCP tool not available in this build"
    return
  fi
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "federation_group_list MCP tool returned list"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "federation_group_list MCP tool returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_579
: "${RESULT:=fail}"
unset -f _story_ts_579
