#!/usr/bin/env bash
# TS-047 — research_sessions MCP tool
# tags: surface:mcp feature:memory
# legacy fn: t5_ts047_research_sessions_mcp
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-047"
story_preflight "surface:mcp feature:memory" || return 0

_story_ts_047() {
  if [[ "$(t5_check_memory)" != "yes" ]]; then skip "memory subsystem not enabled"; return; fi
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"research_sessions","params":{"query":"test","limit":5}}')
  save_evidence TS-047 "research.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "research_sessions MCP call returned dict"
  else
    ko "research_sessions failed: $resp"
  fi
}

RESULT=fail
_story_ts_047
: "${RESULT:=fail}"
unset -f _story_ts_047
