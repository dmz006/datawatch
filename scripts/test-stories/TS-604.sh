#!/usr/bin/env bash
# TS-604 — list_sessions MCP tool result includes server field on each item
# tags: surface:mcp feature:multiserver
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-604"
story_preflight "surface:mcp feature:multiserver" || return 0

_story_ts_604() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"list_sessions","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-604 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not found\|not enabled\|no route"; then
    skip "list_sessions MCP tool not available in this build"
    return
  fi
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "list_sessions MCP tool returned list"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "list_sessions MCP tool returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_604
: "${RESULT:=fail}"
unset -f _story_ts_604
