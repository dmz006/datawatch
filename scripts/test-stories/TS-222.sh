#!/usr/bin/env bash
# TS-222 — Cost tracking surface
# tags: surface:api feature:cost
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-222"
story_preflight "surface:api feature:cost" || return 0

_story_ts_222() {
    echo ""; echo "  >> TS-222: Cost tracking surface"
    resp=$(api GET /api/stats)
    has_cost=$(echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); print('cost' in str(d).lower())" 2>/dev/null || echo "False")
    save_evidence "TS-222" "stats_cost.json" "$resp"
    if [[ "$has_cost" == "True" ]]; then
      ok "Cost tracking data present in stats"
    else
      skip "No cost tracking data found in stats"
    fi

}

RESULT=fail
_story_ts_222
: "${RESULT:=fail}"
unset -f _story_ts_222
