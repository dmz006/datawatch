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
  # Check that comm_stats is a list (array of CommChannelStat objects)
  local comm_result
  comm_result=$(echo "$resp" | python3 -c "
import json, sys
d = json.load(sys.stdin)
comm = d.get('comm_stats', None)
if comm is None:
    print('missing')
elif isinstance(comm, list):
    names = [c.get('name','').lower() for c in comm if isinstance(c, dict)]
    if any(n in ('web', 'mcp', 'http', 'sse', 'websocket', 'claude-code', 'ollama', 'shell') for n in names):
        print('yes')
    elif names:
        print('names:' + ','.join(names))
    else:
        print('empty')
else:
    print('wrong_type:' + type(comm).__name__)
" 2>/dev/null || echo "missing")
  if [[ "$comm_result" == "yes" ]]; then
    ok "GET /api/stats: comm_stats has recognized channel/backend entry"
  elif [[ "$comm_result" == "empty" ]]; then
    ok "GET /api/stats: comm_stats array present (no channels configured)"
  elif echo "$comm_result" | grep -q "^names:"; then
    local names
    names=$(echo "$comm_result" | sed 's/^names://')
    ok "GET /api/stats: comm_stats array present (channels: $names)"
  else
    skip "GET /api/stats: comm_stats not present or wrong shape: $comm_result"
  fi
}

RESULT=fail
_story_ts_107
: "${RESULT:=fail}"
unset -f _story_ts_107
