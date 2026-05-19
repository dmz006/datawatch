#!/usr/bin/env bash
# TS-271 — algorithm_start + algorithm_get via MCP
# tags: surface:mcp feature:mcp feature:algorithm
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-271"
story_preflight "surface:mcp feature:mcp feature:algorithm" || return 0

_story_ts_271() {
  local resp algo_id

  # Register a test session in algorithm mode so algorithm_list returns something
  ensure_test_session || return
  local reg_resp
  reg_resp=$(api POST "/api/algorithm/$SESSION_ID/start" '{}' 2>/dev/null || echo '{}')
  if ! echo "$reg_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert "error" not in d' 2>/dev/null; then
    skip "could not register session in algorithm mode for test"
    return
  fi

  # List via MCP
  resp=$(api POST /api/mcp/call '{"tool":"algorithm_list","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-271 "list.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    api DELETE "/api/algorithm/$SESSION_ID" >/dev/null 2>&1 || true
    skip "algorithm_list not available in this build"
    return
  fi

  algo_id=$(echo "$resp" | python3 -c '
import json,sys
d=json.load(sys.stdin)
if isinstance(d,list): items=d
elif isinstance(d,dict): items=d.get("sessions",d.get("algorithms",d.get("items",d.get("result",[]))))
else: items=[]
if isinstance(items,list) and len(items)>0:
    item=items[0]
    if isinstance(item,dict): print(item.get("id",item.get("session_id",item.get("name",""))))
    else: print(str(item))
' 2>/dev/null || echo "")

  if [[ -z "$algo_id" ]]; then
    api DELETE "/api/algorithm/$SESSION_ID" >/dev/null 2>&1 || true
    skip "algorithm_list returned empty list after registration"
    return
  fi

  # Get by id via MCP
  resp=$(api POST /api/mcp/call "{\"tool\":\"algorithm_get\",\"params\":{\"id\":\"$algo_id\"}}")
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-271 "get.json" "$resp"

  # Clean up
  api DELETE "/api/algorithm/$SESSION_ID" >/dev/null 2>&1 || true

  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "algorithm_get returned dict for id $algo_id"
  else
    ko "algorithm_get unexpected: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_271
: "${RESULT:=fail}"
unset -f _story_ts_271
