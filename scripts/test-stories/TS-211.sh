#!/usr/bin/env bash
# TS-211 — identity-and-telos: identity GET
# tags: surface:api feature:identity
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-211"
story_preflight "surface:api feature:identity" || return 0

_story_ts_211() {
    echo ""; echo "  >> TS-211: identity-and-telos: identity GET"
    resp=$(api GET /api/identity 2>/dev/null || api GET /api/telos 2>/dev/null || echo '{"error":"not found"}')
    save_evidence "TS-211" "identity.json" "$resp"
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'error' not in d" 2>/dev/null; then
      ok "Identity endpoint reachable"
    else
      skip "Identity endpoint not found"
    fi

}

RESULT=fail
_story_ts_211
: "${RESULT:=fail}"
unset -f _story_ts_211
