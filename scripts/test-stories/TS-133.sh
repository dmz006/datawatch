#!/usr/bin/env bash
# TS-133 — Stats panel shows live data
# tags: surface:pwa feature:bootstrap conflict:pwa
# legacy fn: t11_ts133_stats_panel
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-133"
story_preflight "surface:pwa feature:bootstrap conflict:pwa" || return 0

_story_ts_133() {
  local resp
  resp=$(api GET /api/stats)
  save_evidence TS-133 "stats.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "stats endpoint returns data"
  else
    ko "stats endpoint failed: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_133
: "${RESULT:=fail}"
unset -f _story_ts_133
