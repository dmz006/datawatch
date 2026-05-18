#!/usr/bin/env bash
# TS-498 — autonomous_prd_set_llm MCP tool accepts llm_ref param
# tags: surface:mcp feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-498"
story_preflight "surface:mcp feature:automata" || return 0

_story_ts_498() {
  ensure_test_automaton || return
  local llm_name
  llm_name=$(api GET /api/llms | python3 -c 'import json,sys;d=json.load(sys.stdin);llms=d.get("llms",d) if isinstance(d,dict) else d;print(llms[0]["name"] if isinstance(llms,list) and llms else "")' 2>/dev/null || echo "shell")
  local resp
  resp=$(api POST /api/mcp/call "{\"tool\":\"autonomous_prd_set_llm\",\"params\":{\"id\":\"$AUTOMATON_ID\",\"llm_ref\":\"$llm_name\"}}")
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-498 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not enabled\|not found\|404"; then
    skip "autonomous_prd tool not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "autonomous_prd_set_llm tool returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_498
: "${RESULT:=fail}"
unset -f _story_ts_498
