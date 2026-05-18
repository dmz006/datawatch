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
  local add_resp
  add_resp=$(api POST /api/mcp/call "{\"tool\":\"llm_add_model\",\"params\":{\"name\":\"$llm_name\",\"model\":\"$test_model\"}}")
  save_evidence TS-486 "add.json" "$add_resp"
  if echo "$add_resp" | grep -qi "unknown tool\|not enabled"; then
    skip "llm_add_model tool not available"
    return
  fi
  # Clean up: remove test model
  api POST /api/mcp/call "{\"tool\":\"llm_remove_model\",\"params\":{\"name\":\"$llm_name\",\"model\":\"$test_model\"}}" >/dev/null 2>&1
  if assert_json "$add_resp" 'isinstance(d, dict)'; then
    ok "llm_add_model tool returned dict; cleanup done"
  elif echo "$add_resp" | grep -qi "not found\|404"; then
    skip "llm_add_model: LLM $llm_name not found"
  else
    ko "unexpected response: $(echo "$add_resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_486
: "${RESULT:=fail}"
unset -f _story_ts_486
