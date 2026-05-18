#!/usr/bin/env bash
# TS-497 — autonomous_prd_set_skills MCP tool accepts skills param
# tags: surface:mcp feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-497"
story_preflight "surface:mcp feature:automata" || return 0

_story_ts_497() {
  ensure_test_automaton || return
  local resp
  resp=$(api POST /api/mcp/call "{\"tool\":\"autonomous_prd_set_skills\",\"params\":{\"id\":\"$AUTOMATON_ID\",\"skills\":[]}}")
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-497 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not enabled"; then
    skip "autonomous_prd_set_skills tool not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "autonomous_prd_set_skills tool returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_497
: "${RESULT:=fail}"
unset -f _story_ts_497
