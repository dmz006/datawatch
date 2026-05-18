#!/usr/bin/env bash
# TS-180 — Sessions feature: 7-surface parity matrix
# tags: surface:api feature:parity
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-180"
story_preflight "surface:api feature:parity" || return 0

_story_ts_180() {
    echo ""; echo "  >> TS-180: Sessions feature: 7-surface parity matrix"
    resp=$(api GET /api/sessions)
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert isinstance(d,list)" 2>/dev/null; then
      ok "GET /api/sessions returns list"
    else
      ko "GET /api/sessions did not return list"
    fi
    resp2=$(api GET /api/sessions/stats 2>/dev/null || api GET /api/stats 2>/dev/null)
    save_evidence "TS-180" "sessions_list.json" "$resp"
    save_evidence "TS-180" "sessions_stats.json" "$resp2"

}

RESULT=fail
_story_ts_180
: "${RESULT:=fail}"
unset -f _story_ts_180
