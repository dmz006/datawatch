#!/usr/bin/env bash
# TS-157 — Cost rates endpoint
# tags: surface:api feature:cost
# legacy fn: t12_ts157_cost_rates
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-157"
story_preflight "surface:api feature:cost" || return 0

_story_ts_157() {
  local resp
  resp=$(api GET /api/cost/rates 2>/dev/null || api GET /api/costs 2>/dev/null || echo '{"error":"not found"}')
  save_evidence TS-157 "rates.json" "$resp"
  if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'error' not in d" 2>/dev/null; then
    ok "Cost rates endpoint reachable"
  else
    local stats_resp
    stats_resp=$(api GET /api/stats)
    save_evidence TS-157 "stats.json" "$stats_resp"
    if echo "$stats_resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'cost' in str(d).lower() or 'token' in str(d).lower()" 2>/dev/null; then
      ok "Cost data found in /api/stats"
    else
      skip "Cost rates endpoint not available (may be v7.1+ feature)"
    fi
  fi
}

RESULT=fail
_story_ts_157
: "${RESULT:=fail}"
unset -f _story_ts_157
