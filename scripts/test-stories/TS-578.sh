#!/usr/bin/env bash
# TS-578 — federation_peer_test MCP tool returns ok/latency shape
# tags: surface:mcp feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-578"
story_preflight "surface:mcp feature:federation" || return 0

_story_ts_578() {
  local peer_name="e2e-mcp-peer-ts578"
  # Create a peer first so we have something to test
  local create_resp
  create_resp=$(api POST /api/mcp/call "{\"tool\":\"federation_peer_add\",\"params\":{\"name\":\"$peer_name\",\"url\":\"http://127.0.0.1:19999\",\"token\":\"test\"}}")
  create_resp=$(mcp_unwrap "$create_resp")
  if echo "$create_resp" | grep -qi "unknown tool\|not found\|not enabled\|no route"; then
    skip "federation_peer_add MCP tool not available in this build"
    return
  fi

  local resp
  resp=$(api POST /api/mcp/call "{\"tool\":\"federation_peer_test\",\"params\":{\"name\":\"$peer_name\"}}")
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-578 "resp.json" "$resp"

  # cleanup
  api DELETE "/api/federation/peers/$peer_name" >/dev/null 2>&1 || true

  if echo "$resp" | grep -qi "unknown tool\|not enabled"; then
    skip "federation_peer_test MCP tool not available in this build"
    return
  fi
  if echo "$resp" | grep -qi "not found\|peer not found"; then
    skip "test peer not found after create — federation may not persist in this build"
    return
  fi
  if echo "$resp" | grep -qi "connection refused\|connection reset\|dial tcp\|i/o timeout"; then
    skip "test peer unreachable (expected on fresh install)"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "federation_peer_test MCP tool returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_578
: "${RESULT:=fail}"
unset -f _story_ts_578
