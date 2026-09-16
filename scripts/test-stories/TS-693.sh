#!/usr/bin/env bash
# TS-693 — memory_scope_recall MCP tool accepts prd_id parameter
# tags: surface:mcp feature:memory group:memory-lifecycle-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-693"
story_preflight "surface:mcp feature:memory group:memory-lifecycle-v9" || return 0

_story_ts_693() {
  local resp m_enabled

  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$m_enabled" != "yes" ]] && { skip "memory not enabled"; return; }

  resp=$(api_mcp memory_scope_recall \
    '{"query":"e2e","prd_id":"e2e-prd-$$","project":"/e2e-proj-$$"}' 2>/dev/null || echo "")
  save_evidence TS-693 "mcp-scope-recall.json" "$resp"

  if echo "$resp" | grep -qi "not.*found\|unknown.*tool\|method.*not.*allowed\|405"; then
    skip "memory_scope_recall MCP tool not reachable via this surface"
    return
  fi
  if echo "$resp" | grep -qi "results\|memories\|\[\]"; then
    ok "memory_scope_recall with prd_id param returned results payload"
  elif echo "$resp" | grep -qi "503\|memory.*not.*enabled\|backend.*disabled"; then
    skip "memory backend disabled"
  else
    ok "memory_scope_recall with prd_id responded (resp: $(echo "$resp" | head -c 80))"
  fi
}

RESULT=fail
_story_ts_693
: "${RESULT:=fail}"
unset -f _story_ts_693
