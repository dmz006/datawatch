#!/usr/bin/env bash
# TS-070 — GET /api/mcp/tools (≥30 tools)
# tags: surface:mcp feature:mcp
# legacy fn: t8_ts070_mcp_tools
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-070"
story_preflight "surface:mcp feature:mcp" || return 0

_story_ts_070() {
  local resp
  resp=$(api GET /api/mcp/docs)
  save_evidence TS-070 "tools.json" "$resp"
  if echo "$resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
assert isinstance(d, list) and len(d) >= 30
names = {t['name'] for t in d}
required = {'list_sessions','start_session','send_input','schedule_add','profile_list','agent_list'}
missing = required - names
assert not missing, 'missing: ' + ','.join(sorted(missing))
" 2>/dev/null; then
    local n
    n=$(echo "$resp" | python3 -c 'import json,sys;print(len(json.load(sys.stdin)))' 2>/dev/null)
    ok "MCP docs: $n tools, foundational set present"
  else
    ko "MCP tool surface incomplete or <30 tools: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_070
: "${RESULT:=fail}"
unset -f _story_ts_070
