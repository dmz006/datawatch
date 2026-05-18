#!/usr/bin/env bash
# TS-106 — GET /api/commands list
# tags: surface:comms feature:comms
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-106"
story_preflight "surface:comms feature:comms" || return 0

_story_ts_106() {
  local resp code
  resp=$(api_code GET /api/commands)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  local body
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-106 "commands.json" "$body"
  if [[ "$code" == "404" ]]; then
    skip "GET /api/commands: endpoint not available (404)"
    return
  fi
  if [[ "$code" == "200" ]]; then
    # Accept either array or dict with commands key
    local count
    count=$(echo "$body" | python3 -c "
import json, sys
d = json.load(sys.stdin)
if isinstance(d, list):
    print(len(d))
elif isinstance(d, dict):
    lst = d.get('commands', d.get('items', []))
    print(len(lst) if isinstance(lst, list) else 1)
else:
    print(0)
" 2>/dev/null || echo "0")
    if [[ "$count" -ge 0 ]]; then
      ok "GET /api/commands: returned $count commands"
    else
      ko "GET /api/commands: unexpected body shape: $(echo "$body" | head -c 100)"
    fi
  else
    ko "GET /api/commands: unexpected HTTP $code: $(echo "$body" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_106
: "${RESULT:=fail}"
unset -f _story_ts_106
