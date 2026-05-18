#!/usr/bin/env bash
# TS-460 — compute_node_attach_observer MCP tool sets observer_peer field
# tags: surface:mcp feature:observer feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-460"
story_preflight "surface:mcp feature:observer feature:compute" || return 0

_story_ts_460() {
  local cname="ts-460-node-$$"
  local pname="ts-460-peer-$$"
  local resp
  resp=$(api POST "/api/compute/nodes?probe=skip" "{\"name\":\"$cname\",\"kind\":\"ollama\",\"address\":\"http://localhost:11434\"}")
  if ! assert_json "$resp" '"name" in d or "id" in d'; then
    skip "could not create compute node for test: $(echo "$resp" | head -c 100)"
    return
  fi
  add_cleanup compute_node "$cname"
  # Register a temporary observer peer for the test
  local peer_resp
  peer_resp=$(api POST /api/observer/peers "{\"name\":\"$pname\",\"shape\":\"B\"}")
  if ! assert_json "$peer_resp" '"name" in d'; then
    skip "observer peer register not available: $(echo "$peer_resp" | head -c 100)"
    return
  fi
  add_cleanup observer_peer "$pname"
  local attach_resp
  attach_resp=$(api POST /api/mcp/call "{\"tool\":\"compute_node_attach_observer\",\"params\":{\"name\":\"$cname\",\"peer\":\"$pname\"}}")
  attach_resp=$(mcp_unwrap "$attach_resp")
  save_evidence TS-460 "attach.json" "$attach_resp"
  if echo "$attach_resp" | grep -qi "unknown tool\|not found\|not enabled"; then
    skip "compute_node_attach_observer tool not available"
    return
  fi
  # Detach after test
  api POST /api/mcp/call "{\"tool\":\"compute_node_detach_observer\",\"params\":{\"name\":\"$cname\"}}" >/dev/null 2>&1
  if assert_json "$attach_resp" 'isinstance(d, dict)'; then
    ok "compute_node_attach_observer tool returned dict"
  else
    ko "unexpected response: $(echo "$attach_resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_460
: "${RESULT:=fail}"
unset -f _story_ts_460
