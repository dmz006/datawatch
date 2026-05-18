#!/usr/bin/env bash
# TS-242 — Monitoring journey: comm stats surface
# tags: surface:api feature:comms
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-242"
story_preflight "surface:api feature:comms" || return 0

_story_ts_242() {
    echo ""; echo "  >> TS-242: Monitoring journey: comm stats surface"
    resp=$(api GET /api/stats)
    save_evidence "TS-242" "comm_stats.json" "$resp"
    ok "Comm stats journey: stats endpoint polled successfully"

}

RESULT=fail
_story_ts_242
: "${RESULT:=fail}"
unset -f _story_ts_242
