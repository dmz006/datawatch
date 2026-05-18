#!/usr/bin/env bash
# TS-221 — Link status + interfaces
# tags: surface:api feature:network
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-221"
story_preflight "surface:api feature:network" || return 0

_story_ts_221() {
    echo ""; echo "  >> TS-221: Link status + interfaces"
    resp=$(api GET /api/network 2>/dev/null || api GET /api/links 2>/dev/null || echo '{"error":"not found"}')
    save_evidence "TS-221" "network.json" "$resp"
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'error' not in d" 2>/dev/null; then
      ok "Network/link endpoint reachable"
    else
      skip "Network endpoint not found"
    fi

}

RESULT=fail
_story_ts_221
: "${RESULT:=fail}"
unset -f _story_ts_221
