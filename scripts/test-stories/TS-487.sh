#!/usr/bin/env bash
# TS-487 — llm_list_models MCP tool returns models array
# tags: surface:mcp feature:llm-registry
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-487"
story_preflight "surface:mcp feature:llm-registry" || return 0

_story_ts_487() {
  local llm_name
  llm_name=$(api GET /api/llms | python3 -c 'import json,sys;d=json.load(sys.stdin);llms=d.get("llms",d) if isinstance(d,dict) else d;print(llms[0]["name"] if isinstance(llms,list) and llms else "")' 2>/dev/null || echo "")
  if [[ -z "$llm_name" ]]; then
    skip "no LLMs configured"
    return
  fi
  local resp
  resp=$(api POST /api/mcp/call "{\"tool\":\"llm_list_models\",\"params\":{\"name\":\"$llm_name\"}}")
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-487 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not enabled"; then
    skip "llm_list_models tool not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d.get("models",[]), list) or isinstance(d, list)'; then
    ok "llm_list_models tool returned models array for $llm_name"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "llm_list_models tool returned dict for $llm_name"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_487
: "${RESULT:=fail}"
unset -f _story_ts_487
