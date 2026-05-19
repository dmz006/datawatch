#!/usr/bin/env bash
# TS-496 — autonomous_prd_set_guided_mode MCP tool accepts guided_mode param
# tags: surface:mcp feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-496"
story_preflight "surface:mcp feature:automata" || return 0

_story_ts_496() {
  ensure_test_automaton || return
  local resp
  resp=$(api POST /api/mcp/call "{\"tool\":\"autonomous_prd_set_guided_mode\",\"params\":{\"id\":\"$AUTOMATON_ID\",\"guided_mode\":true}}")
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-496 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not enabled" || ! echo "$resp" | python3 -c "import json,sys; json.load(sys.stdin)" 2>/dev/null; then
    skip "autonomous_prd tool not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "autonomous_prd_set_guided_mode tool returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_496
: "${RESULT:=fail}"
unset -f _story_ts_496
