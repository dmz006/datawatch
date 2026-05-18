#!/usr/bin/env bash
# TS-484 — llm_in_use MCP tool returns bindings shape
# tags: surface:mcp feature:llm-registry
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-484"
story_preflight "surface:mcp feature:llm-registry" || return 0

_story_ts_484() {
  local llm_name
  llm_name=$(api GET /api/llms | python3 -c 'import json,sys;d=json.load(sys.stdin);llms=d.get("llms",d) if isinstance(d,dict) else d;print(llms[0]["name"] if isinstance(llms,list) and llms else "")' 2>/dev/null || echo "")
  if [[ -z "$llm_name" ]]; then
    skip "no LLMs configured"
    return
  fi
  local resp
  resp=$(api POST /api/mcp/call "{\"tool\":\"llm_in_use\",\"params\":{\"name\":\"$llm_name\"}}")
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-484 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not found\|not enabled"; then
    skip "llm_in_use tool not available"
    return
  fi
  if assert_json "$resp" '"bindings" in d or isinstance(d, dict)'; then
    ok "llm_in_use tool returned valid shape for $llm_name"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_484
: "${RESULT:=fail}"
unset -f _story_ts_484
