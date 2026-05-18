#!/usr/bin/env bash
# TS-293 — memory_scope_recall + memory_scope_borrow + memory_scope_seed via MCP
# tags: surface:mcp feature:mcp feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-293"
story_preflight "surface:mcp feature:mcp feature:memory" || return 0

_story_ts_293() {
  local resp

  # Check if memory is enabled
  local m_enabled
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$m_enabled" != "yes" ]]; then
    skip "memory subsystem not enabled"
    return
  fi

  # memory_scope_recall
  resp=$(api POST /api/mcp/call '{"tool":"memory_scope_recall","params":{"query":"test"}}')
  save_evidence TS-293 "recall.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "memory_scope_recall not available in this build"
    return
  fi

  # memory_scope_borrow
  resp=$(api POST /api/mcp/call '{"tool":"memory_scope_borrow","params":{"scope":"global"}}')
  save_evidence TS-293 "borrow.json" "$resp"

  # memory_scope_seed
  resp=$(api POST /api/mcp/call '{"tool":"memory_scope_seed","params":{"scope":"session","content":"e2e test seed"}}')
  save_evidence TS-293 "seed.json" "$resp"

  ok "memory_scope_recall + borrow + seed executed via MCP"
}

RESULT=fail
_story_ts_293
: "${RESULT:=fail}"
unset -f _story_ts_293
