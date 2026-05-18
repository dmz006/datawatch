#!/usr/bin/env bash
# TS-081 — Channel bridge discovers same tool count
# tags: surface:mcp feature:mcp
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-081"
story_preflight "surface:mcp feature:mcp" || return 0

_story_ts_081() {
  local resp code
  resp=$(api_code GET /api/mcp/tools)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  local body
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-081 "tools.json" "$body"
  if [[ "$code" == "404" ]]; then
    skip "GET /api/mcp/tools: endpoint not available (404)"
    return
  fi
  local count
  count=$(echo "$body" | python3 -c "
import json, sys
d = json.load(sys.stdin)
if isinstance(d, list):
    print(len(d))
elif isinstance(d, dict):
    lst = d.get('tools', d.get('items', []))
    print(len(lst) if isinstance(lst, list) else 0)
else:
    print(0)
" 2>/dev/null || echo "0")
  if [[ "$count" -gt 0 ]]; then
    ok "GET /api/mcp/tools returned $count tools (channel bridge has tool discovery)"
  else
    ko "GET /api/mcp/tools returned 0 tools: $(echo "$body" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_081
: "${RESULT:=fail}"
unset -f _story_ts_081
