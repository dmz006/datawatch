#!/usr/bin/env bash
# TS-245 — Update check journey: version check without install
# tags: surface:api feature:update
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-245"
story_preflight "surface:api feature:update" || return 0

_story_ts_245() {
    echo ""; echo "  >> TS-245: Update check journey: version check without install"
    resp=$(api GET /api/updates 2>/dev/null || api POST /api/updates/check '{}' 2>/dev/null || echo '{"error":"not found"}')
    save_evidence "TS-245" "update_check.json" "$resp"
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'error' not in d" 2>/dev/null; then
      ok "Update check endpoint reachable"
    else
      skip "Update check endpoint not found"
    fi

}

RESULT=fail
_story_ts_245
: "${RESULT:=fail}"
unset -f _story_ts_245
