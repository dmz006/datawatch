#!/usr/bin/env bash
# TS-071 — POST /api/mcp/call (memory_recall)
# tags: surface:mcp feature:mcp feature:memory
# legacy fn: t8_ts071_mcp_call_memory_recall
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-071"
story_preflight "surface:mcp feature:mcp feature:memory" || return 0

_story_ts_071() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"memory_recall","params":{"query":"test"}}')
  save_evidence TS-071 "recall.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "POST /api/mcp/call memory_recall returned dict"
  else
    ko "MCP call memory_recall failed: $resp"
  fi
}

RESULT=fail
_story_ts_071
: "${RESULT:=fail}"
unset -f _story_ts_071
