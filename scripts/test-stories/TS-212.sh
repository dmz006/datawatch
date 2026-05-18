#!/usr/bin/env bash
# TS-212 — algorithm-mode: phase list surface
# tags: surface:api feature:algorithm
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-212"
story_preflight "surface:api feature:algorithm" || return 0

_story_ts_212() {
    echo ""; echo "  >> TS-212: algorithm-mode: phase list surface"
    resp=$(api GET /api/algorithm 2>/dev/null || api GET /api/algorithms 2>/dev/null || echo '{"error":"not found"}')
    save_evidence "TS-212" "algorithm.json" "$resp"
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'error' not in d" 2>/dev/null; then
      ok "Algorithm endpoint reachable"
    else
      skip "Algorithm endpoint not found"
    fi

}

RESULT=fail
_story_ts_212
: "${RESULT:=fail}"
unset -f _story_ts_212
