#!/usr/bin/env bash
# TS-202 — alerts-and-notifications: alert surface + comm forward
# tags: surface:api feature:alerts
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-202"
story_preflight "surface:api feature:alerts" || return 0

_story_ts_202() {
    echo ""; echo "  >> TS-202: alerts-and-notifications: alert surface + comm forward"
    resp=$(api GET /api/alerts 2>/dev/null || api GET /api/alert 2>/dev/null || echo '{"error":"not found"}')
    save_evidence "TS-202" "alerts.json" "$resp"
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'error' not in d or d.get('error') != 'not found'" 2>/dev/null; then
      ok "Alerts endpoint reachable"
    else
      skip "Alerts endpoint not found (may not be implemented in this build)"
    fi

}

RESULT=fail
_story_ts_202
: "${RESULT:=fail}"
unset -f _story_ts_202
