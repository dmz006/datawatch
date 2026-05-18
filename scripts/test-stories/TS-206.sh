#!/usr/bin/env bash
# TS-206 — channel-state-engine: session state field
# tags: surface:api feature:sessions
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-206"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_206() {
    echo ""; echo "  >> TS-206: channel-state-engine: session state field"
    resp=$(api GET /api/sessions)
    save_evidence "TS-206" "sessions.json" "$resp"
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert isinstance(d,list)" 2>/dev/null; then
      ok "Session list reachable for state engine verification"
    else
      ko "Session list not returned"
    fi

}

RESULT=fail
_story_ts_206
: "${RESULT:=fail}"
unset -f _story_ts_206
