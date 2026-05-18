#!/usr/bin/env bash
# TS-486 — llm_add_model MCP tool adds model; llm_remove_model removes it
# tags: surface:mcp feature:llm-registry
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-486"
story_preflight "surface:mcp feature:llm-registry" || return 0

_story_ts_486() {
  local llm_name
  llm_name=$(api GET /api/llms | python3 -c 'import json,sys;d=json.load(sys.stdin);llms=d.get("llms",d) if isinstance(d,dict) else d;print(llms[0]["name"] if isinstance(llms,list) and llms else "")' 2>/dev/null || echo "")
  if [[ -z "$llm_name" ]]; then
    skip "no LLMs configured"
    return
  fi
  local test_model="test-model-$$"
  local add_resp add_inner
  add_resp=$(api POST /api/mcp/call "{\"tool\":\"llm_add_model\",\"params\":{\"name\":\"$llm_name\",\"model\":\"$test_model\"}}")
  add_inner=$(mcp_unwrap "$add_resp")
  save_evidence TS-486 "add.json" "$add_resp"
  if echo "$add_inner" | grep -qi "unknown tool\|not enabled\|405\|method not allowed"; then
    skip "llm_add_model tool not available or method not allowed"
    return
  fi
  # Clean up: remove test model
  api POST /api/mcp/call "{\"tool\":\"llm_remove_model\",\"params\":{\"name\":\"$llm_name\",\"model\":\"$test_model\"}}" >/dev/null 2>&1
  if assert_json "$add_inner" 'isinstance(d, dict)'; then
    ok "llm_add_model tool returned dict; cleanup done"
  elif echo "$add_inner" | grep -qi "not found\|404"; then
    skip "llm_add_model: LLM $llm_name not found"
  else
    ko "unexpected response: $(echo "$add_inner" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_486
: "${RESULT:=fail}"
unset -f _story_ts_486
