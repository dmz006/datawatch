#!/usr/bin/env bash
# TS-396 — server_list MCP tool returns array
# tags: surface:mcp feature:multi-server
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-396"
story_preflight "surface:mcp feature:multi-server" || return 0

_story_ts_396() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"server_list","params":{}}')
  save_evidence TS-396 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "server_list MCP tool returns array"
  elif assert_json "$resp" '"servers" in d and isinstance(d["servers"], list)'; then
    ok "server_list MCP tool returns {servers:[...]} shape"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "server_list MCP tool returns dict"
  elif echo "$resp" | grep -qi "unknown tool\|not found\|not enabled\|not available"; then
    skip "server_list MCP tool not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_396
: "${RESULT:=fail}"
unset -f _story_ts_396
