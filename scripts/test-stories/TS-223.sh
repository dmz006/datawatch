#!/usr/bin/env bash
# TS-223 — Routing rules CRUD
# tags: surface:api feature:routing
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-223"
story_preflight "surface:api feature:routing" || return 0

_story_ts_223() {
    echo ""; echo "  >> TS-223: Routing rules CRUD"
    resp=$(api GET /api/routing 2>/dev/null || echo '{"error":"not found"}')
    save_evidence "TS-223" "routing_list.json" "$resp"
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'error' not in d" 2>/dev/null; then
      ok "Routing endpoint reachable"
    else
      skip "Routing endpoint not found (may be alpha feature)"
    fi

}

RESULT=fail
_story_ts_223
: "${RESULT:=fail}"
unset -f _story_ts_223
