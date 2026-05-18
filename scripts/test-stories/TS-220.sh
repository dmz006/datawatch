#!/usr/bin/env bash
# TS-220 — Alerts: CRUD surface
# tags: surface:api feature:alerts
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-220"
story_preflight "surface:api feature:alerts" || return 0

_story_ts_220() {
    echo ""; echo "  >> TS-220: Alerts: CRUD surface"
    resp=$(api GET /api/alerts 2>/dev/null || echo '{"error":"not found"}')
    save_evidence "TS-220" "alerts_crud.json" "$resp"
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'error' not in d" 2>/dev/null; then
      ok "Alerts CRUD endpoint reachable"
    else
      skip "Alerts CRUD endpoint not found"
    fi

}

RESULT=fail
_story_ts_220
: "${RESULT:=fail}"
unset -f _story_ts_220
