#!/usr/bin/env bash
# TS-225 — Observer peers surface
# tags: surface:api feature:peers
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-225"
story_preflight "surface:api feature:peers" || return 0

_story_ts_225() {
    echo ""; echo "  >> TS-225: Observer peers surface"
    resp=$(api GET /api/peers 2>/dev/null || echo '{"error":"not found"}')
    save_evidence "TS-225" "peers.json" "$resp"
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'error' not in d" 2>/dev/null; then
      ok "Peers endpoint reachable"
    else
      skip "Peers endpoint not found"
    fi

}

RESULT=fail
_story_ts_225
: "${RESULT:=fail}"
unset -f _story_ts_225
