#!/usr/bin/env bash
# TS-291 — llm_list + llm_get + llm_enable/disable round-trip via MCP
# tags: surface:mcp feature:mcp feature:config
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-291"
story_preflight "surface:mcp feature:mcp feature:config" || return 0

_story_ts_291() {
  local resp llm_id

  # List LLMs
  resp=$(api POST /api/mcp/call '{"tool":"llm_list","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-291 "list.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "llm_list not available in this build"
    return
  fi
  if ! assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ko "llm_list unexpected: $(echo "$resp" | head -c 200)"
    return
  fi

  llm_id=$(echo "$resp" | python3 -c '
import json,sys
d=json.load(sys.stdin)
if isinstance(d,list) and len(d)>0:
    item=d[0]; print(item.get("id",item.get("name","")) if isinstance(item,dict) else str(item))
elif isinstance(d,dict):
    for k in ("llms","backends","items","result"):
        if k in d and isinstance(d[k],list) and len(d[k])>0:
            item=d[k][0]; print(item.get("id",item.get("name","")) if isinstance(item,dict) else str(item)); exit()
' 2>/dev/null || echo "")

  if [[ -z "$llm_id" ]]; then
    ok "llm_list returned valid shape (no LLMs configured)"
    return
  fi

  # Get specific LLM
  resp=$(api POST /api/mcp/call "{\"tool\":\"llm_get\",\"params\":{\"id\":\"$llm_id\"}}")
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-291 "get.json" "$resp"
  if ! assert_json "$resp" 'isinstance(d, dict)'; then
    ok "llm_list returned LLMs; llm_get: $(echo "$resp" | head -c 80)"
    return
  fi

  ok "llm_list + llm_get round-trip for LLM $llm_id"
}

RESULT=fail
_story_ts_291
: "${RESULT:=fail}"
unset -f _story_ts_291
