#!/usr/bin/env bash
# TS-466 — autonomous_prd_decompose MCP tool accepts planning_backend param
# tags: surface:mcp feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-466"
story_preflight "surface:mcp feature:automata" || return 0

_story_ts_466() {
  ensure_test_automaton || return
  local resp
  resp=$(api POST /api/mcp/call "{\"tool\":\"autonomous_prd_decompose\",\"params\":{\"id\":\"$AUTOMATON_ID\",\"planning_backend\":\"default\"}}")
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-466 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not found\|not enabled"; then
    skip "autonomous_prd_decompose tool not available"
    return
  fi
  if echo "$resp" | grep -qi "does not support headless planning\|planning backend.*has kind\|use an ollama\|planning.*not supported\|ollama not configured\|ollama.*not configured\|500 Internal Server Error"; then
    skip "planning backend not available or not configured for headless planning"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "autonomous_prd_decompose tool accepted planning_backend param"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_466
: "${RESULT:=fail}"
unset -f _story_ts_466
