#!/usr/bin/env bash
# TS-041 — memory_recall semantic search
# tags: surface:mcp feature:memory
# legacy fn: t5_ts041_memory_recall_mcp
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-041"
story_preflight "surface:mcp feature:memory" || return 0

_story_ts_041() {
  if [[ "$(t5_check_memory)" != "yes" ]]; then skip "memory subsystem not enabled"; return; fi
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"memory_recall","params":{"query":"v7.0.0 e2e testing"}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-041 "recall.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "memory_recall MCP call returned dict"
  else
    ko "memory_recall failed: $resp"
  fi
}

RESULT=fail
_story_ts_041
: "${RESULT:=fail}"
unset -f _story_ts_041
