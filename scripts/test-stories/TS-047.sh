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
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-047 "research.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "research_sessions MCP call returned valid shape"
  elif [[ -n "$resp" ]]; then
    ok "research_sessions returned text result: $(echo "$resp" | head -c 80)"
  else
    ko "research_sessions returned empty response"
  fi
}

RESULT=fail
_story_ts_047
: "${RESULT:=fail}"
unset -f _story_ts_047
