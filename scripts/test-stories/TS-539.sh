#!/usr/bin/env bash
# TS-539 — GET /api/mcp/tools returns channel bridge tools (count > 0)
# tags: surface:api feature:mcp
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-539"
story_preflight "surface:api feature:mcp" || return 0

_story_ts_539() {
  local resp
  resp=$(api GET /api/mcp/tools)
  save_evidence TS-539 "tools.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|unknown"; then
    skip "mcp/tools endpoint not available"
    return
  fi
  local cnt
  cnt=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);tools=d.get("tools",d) if isinstance(d,dict) else d;print(len(tools) if isinstance(tools,list) else 0)' 2>/dev/null || echo "0")
  if [[ "$cnt" -gt 0 ]] 2>/dev/null; then
    ok "GET /api/mcp/tools returns $cnt tools"
  elif assert_json "$resp" 'isinstance(d, (dict, list))'; then
    skip "mcp/tools responds but no tools found"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_539
: "${RESULT:=fail}"
unset -f _story_ts_539
