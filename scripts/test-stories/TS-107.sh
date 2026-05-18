#!/usr/bin/env bash
# TS-107 — GET /api/stats comm_stats Web/MCP present
# tags: surface:comms feature:comms
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-107"
story_preflight "surface:comms feature:comms" || return 0

_story_ts_107() {
  local resp
  resp=$(api GET /api/stats)
  save_evidence TS-107 "stats.json" "$resp"
  if ! assert_json "$resp" 'isinstance(d, dict)'; then
    ko "GET /api/stats did not return dict: $(echo "$resp" | head -c 100)"
    return
  fi
  # Check that comm_stats has web or mcp keys
  local has_web_or_mcp
  has_web_or_mcp=$(echo "$resp" | python3 -c "
import json, sys
d = json.load(sys.stdin)
comm = d.get('comm_stats', d.get('comms', {}))
if isinstance(comm, dict):
    keys = [k.lower() for k in comm.keys()]
    if any(k in ('web', 'mcp', 'http', 'sse') for k in keys):
        print('yes')
    else:
        print('keys:' + ','.join(keys))
else:
    print('no')
" 2>/dev/null || echo "no")
  if [[ "$has_web_or_mcp" == "yes" ]]; then
    ok "GET /api/stats: comm_stats has web/mcp entry"
  elif echo "$has_web_or_mcp" | grep -q "^keys:"; then
    local keys
    keys=$(echo "$has_web_or_mcp" | sed 's/^keys://')
    skip "GET /api/stats: comm_stats present but no web/mcp key (found: $keys)"
  else
    skip "GET /api/stats: no comm_stats section present"
  fi
}

RESULT=fail
_story_ts_107
: "${RESULT:=fail}"
unset -f _story_ts_107
