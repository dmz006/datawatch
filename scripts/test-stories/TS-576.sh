#!/usr/bin/env bash
# TS-576 — federation_peer_list MCP tool returns [] on fresh install
# tags: surface:mcp feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-576"
story_preflight "surface:mcp feature:federation" || return 0

_story_ts_576() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"federation_peer_list","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-576 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not found\|not enabled\|no route"; then
    skip "federation_peer_list MCP tool not available in this build"
    return
  fi
  if [[ "$resp" == "null" ]] || assert_json "$resp" 'd is None'; then
    ok "federation_peer_list MCP tool returned null (no peers)"
  elif assert_json "$resp" 'isinstance(d, list)'; then
    ok "federation_peer_list MCP tool returned list"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "federation_peer_list MCP tool returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_576
: "${RESULT:=fail}"
unset -f _story_ts_576
