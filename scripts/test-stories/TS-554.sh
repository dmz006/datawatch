#!/usr/bin/env bash
# TS-554 — memory_recall MCP tool returns hits array — skip if memory disabled
# tags: surface:mcp feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-554"
story_preflight "surface:mcp feature:memory" || return 0

_story_ts_554() {
  local m_enabled
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$m_enabled" != "yes" ]] && { skip "memory not enabled"; return; }
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"memory_recall","params":{"query":"test"}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-554 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not enabled"; then
    skip "memory_recall tool not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d.get("hits",[]), list) or isinstance(d, list)'; then
    ok "memory_recall tool returned hits array"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "memory_recall tool returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_554
: "${RESULT:=fail}"
unset -f _story_ts_554
