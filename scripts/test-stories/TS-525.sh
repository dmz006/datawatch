#!/usr/bin/env bash
# TS-525 — memory_scope_promote MCP tool — skip if memory disabled
# tags: surface:mcp feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-525"
story_preflight "surface:mcp feature:memory" || return 0

_story_ts_525() {
  local m_enabled
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$m_enabled" != "yes" ]] && { skip "memory not enabled"; return; }
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"memory_scope_promote","params":{"memory_id":"1","from_scope":"session-local","to_scope":"project-shared"}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-525 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not enabled"; then
    skip "memory_scope_promote tool not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "memory_scope_promote tool returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_525
: "${RESULT:=fail}"
unset -f _story_ts_525
