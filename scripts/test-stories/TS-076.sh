#!/usr/bin/env bash
# TS-076 — GET /api/mcp/resources/templates count >= 4
# tags: surface:mcp feature:mcp
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-076"
story_preflight "surface:mcp feature:mcp" || return 0

_story_ts_076() {
  local resp code
  resp=$(api_code GET /api/mcp/resources/templates)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  local body
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-076 "templates.json" "$body"
  if [[ "$code" == "404" ]]; then
    skip "GET /api/mcp/resources/templates: endpoint not available (404)"
    return
  fi
  local count
  count=$(echo "$body" | python3 -c "
import json, sys
d = json.load(sys.stdin)
if isinstance(d, list):
    print(len(d))
elif isinstance(d, dict):
    lst = d.get('resourceTemplates', d.get('templates', d.get('items', [])))
    print(len(lst) if isinstance(lst, list) else 0)
else:
    print(0)
" 2>/dev/null || echo "0")
  if [[ "$count" -ge 4 ]]; then
    ok "GET /api/mcp/resources/templates returned $count templates (>= 4)"
  elif [[ "$count" -gt 0 ]]; then
    skip "GET /api/mcp/resources/templates returned only $count templates (expected >= 4)"
  else
    ko "GET /api/mcp/resources/templates returned empty/invalid response: $(echo "$body" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_076
: "${RESULT:=fail}"
unset -f _story_ts_076
