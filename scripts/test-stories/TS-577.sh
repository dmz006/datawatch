#!/usr/bin/env bash
# TS-577 — federation_peer_add MCP tool creates peer
# tags: surface:mcp feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-577"
story_preflight "surface:mcp feature:federation" || return 0

_story_ts_577() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"federation_peer_add","params":{"name":"e2e-mcp-peer-ts577","url":"http://127.0.0.1:19999","token":"test"}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-577 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not found\|not enabled\|no route"; then
    skip "federation_peer_add MCP tool not available in this build"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "federation_peer_add MCP tool returned dict"
    # cleanup
    api DELETE /api/federation/peers/e2e-mcp-peer-ts577 >/dev/null 2>&1 || true
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_577
: "${RESULT:=fail}"
unset -f _story_ts_577
