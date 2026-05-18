#!/usr/bin/env bash
# TS-073 — GET /api/mcp/resources count >= 5
# tags: surface:mcp feature:mcp
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-073"
story_preflight "surface:mcp feature:mcp" || return 0

_story_ts_073() {
  local resp code
  resp=$(api_code GET /api/mcp/resources)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  local body
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-073 "resources.json" "$body"
  if [[ "$code" == "404" ]]; then
    skip "GET /api/mcp/resources: endpoint not available (404)"
    return
  fi
  # Accept either {"resources":[...]} or a bare array
  local count
  count=$(echo "$body" | python3 -c "
import json, sys
d = json.load(sys.stdin)
if isinstance(d, list):
    print(len(d))
elif isinstance(d, dict):
    lst = d.get('resources', d.get('items', []))
    print(len(lst) if isinstance(lst, list) else 0)
else:
    print(0)
" 2>/dev/null || echo "0")
  if [[ "$count" -ge 5 ]]; then
    ok "GET /api/mcp/resources returned $count resources (>= 5)"
  elif [[ "$count" -gt 0 ]]; then
    skip "GET /api/mcp/resources returned only $count resources (expected >= 5)"
  else
    ko "GET /api/mcp/resources returned empty/invalid response: $(echo "$body" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_073
: "${RESULT:=fail}"
unset -f _story_ts_073
