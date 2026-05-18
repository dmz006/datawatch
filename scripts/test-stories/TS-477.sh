#!/usr/bin/env bash
# TS-477 — autonomous_prd_approve MCP tool returns dict
# tags: surface:mcp feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-477"
story_preflight "surface:mcp feature:automata" || return 0

_story_ts_477() {
  ensure_test_automaton || return
  local resp
  resp=$(api POST /api/mcp/call "{\"tool\":\"autonomous_prd_approve\",\"params\":{\"id\":\"$AUTOMATON_ID\"}}")
  save_evidence TS-477 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not found\|not enabled"; then
    skip "autonomous_prd_approve tool not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "autonomous_prd_approve tool returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_477
: "${RESULT:=fail}"
unset -f _story_ts_477
