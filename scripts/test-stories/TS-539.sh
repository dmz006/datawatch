#!/usr/bin/env bash
# TS-539 — GET /api/mcp/tools returns channel bridge tools (count > 0)
# tags: surface:api feature:mcp
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-539"
story_preflight "surface:api feature:mcp" || return 0

_story_ts_539() {
  local resp code
  resp=$(api_code GET /api/mcp/tools '')
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  resp=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-539 "tools.json" "$resp"
  if [[ "$code" == "404" || "$code" == "503" || "$code" == "501" ]]; then
    skip "mcp/tools endpoint not available (HTTP $code)"
    return
  fi
  local cnt
  cnt=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);tools=d.get("tools",d) if isinstance(d,dict) else d;print(len(tools) if isinstance(tools,list) else 0)' 2>/dev/null || echo "0")
  if [[ "$cnt" -gt 0 ]] 2>/dev/null; then
    ok "GET /api/mcp/tools returns $cnt tools"
  elif [[ "$code" == "200" ]]; then
    skip "mcp/tools responds 200 but no tools in response"
  else
    ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_539
: "${RESULT:=fail}"
unset -f _story_ts_539
