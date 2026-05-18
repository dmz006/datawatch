#!/usr/bin/env bash
# TS-190 — Comm stats parity: enabled comms in /api/stats
# tags: surface:api feature:parity
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-190"
story_preflight "surface:api feature:parity" || return 0

_story_ts_190() {
    echo ""; echo "  >> TS-190: Comm stats parity: enabled comms in /api/stats"
    resp=$(api GET /api/stats)
    save_evidence "TS-190" "stats.json" "$resp"
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'comm_stats' in d or 'comms' in d or 'sessions' in d" 2>/dev/null; then
      ok "Stats endpoint returns expected structure"
    else
      ko "Stats endpoint missing comm or session data"
    fi
}

RESULT=fail
_story_ts_190
: "${RESULT:=fail}"
unset -f _story_ts_190
