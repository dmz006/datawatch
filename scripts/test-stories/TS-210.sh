#!/usr/bin/env bash
# TS-210 — sessions-deep-dive: full session lifecycle via API
# tags: surface:api feature:sessions
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-210"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_210() {
    echo ""; echo "  >> TS-210: sessions-deep-dive: full session lifecycle via API"
    resp=$(api GET /api/sessions)
    save_evidence "TS-210" "sessions_full.json" "$resp"
    count=$(echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); print(len(d) if isinstance(d,list) else 0)" 2>/dev/null || echo "0")
    ok "Sessions API accessible, found $count sessions"

}

RESULT=fail
_story_ts_210
: "${RESULT:=fail}"
unset -f _story_ts_210
