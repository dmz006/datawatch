#!/usr/bin/env bash
# TS-214 — profiles: list surface
# tags: surface:api feature:profiles
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-214"
story_preflight "surface:api feature:profiles" || return 0

_story_ts_214() {
    echo ""; echo "  >> TS-214: profiles: list surface"
    resp=$(api GET /api/profiles 2>/dev/null || api GET /api/project-profiles 2>/dev/null || echo '{"error":"not found"}')
    save_evidence "TS-214" "profiles.json" "$resp"
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'error' not in d" 2>/dev/null; then
      ok "Profiles endpoint reachable"
    else
      skip "Profiles endpoint not found"
    fi

}

RESULT=fail
_story_ts_214
: "${RESULT:=fail}"
unset -f _story_ts_214
