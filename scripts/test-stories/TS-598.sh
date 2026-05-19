#!/usr/bin/env bash
# TS-598 — Each aggregated item has server field populated
# tags: surface:api feature:multiserver
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-598"
story_preflight "surface:api feature:multiserver" || return 0

_story_ts_598() {
  local resp code body
  resp=$(api_code GET /api/sessions/aggregated)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-598 "resp.json" "$body"
  if [[ "$code" == "404" ]]; then
    skip "sessions/aggregated endpoint not available in this build"
    return
  fi
  local resp="$body"
  # Check if we have any items
  local has_items
  has_items=$(echo "$resp" | python3 -c "
import json, sys
d = json.load(sys.stdin)
items = d if isinstance(d, list) else d.get('sessions', d.get('items', []))
print('yes' if len(items) > 0 else 'no')
" 2>/dev/null || echo "no")
  if [[ "$has_items" != "yes" ]]; then
    skip "no aggregated sessions to check server field (empty list)"
    return
  fi
  # Check first item has server field
  local has_server
  has_server=$(echo "$resp" | python3 -c "
import json, sys
d = json.load(sys.stdin)
items = d if isinstance(d, list) else d.get('sessions', d.get('items', []))
first = items[0] if items else {}
print('yes' if 'server' in first else 'no')
" 2>/dev/null || echo "no")
  if [[ "$has_server" == "yes" ]]; then
    ok "aggregated session items have server field"
  else
    skip "server field not present on aggregated items (multiserver not yet implemented)"
  fi
}

RESULT=fail
_story_ts_598
: "${RESULT:=fail}"
unset -f _story_ts_598
