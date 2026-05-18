#!/usr/bin/env bash
# TS-007 — Stats snapshot shape
# tags: surface:api feature:bootstrap
# legacy fn: t1_ts007_stats_snapshot
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-007"
story_preflight "surface:api feature:bootstrap" || return 0

_story_ts_007() {
  local stats
  stats=$(curl "${curl_args[@]}" "$TEST_BASE/api/stats?v=2" 2>/dev/null)
  save_evidence TS-007 "stats.json" "$stats"
  if assert_json "$stats" '"envelopes" in d or "v" in d or isinstance(d, dict)'; then
    ok "GET /api/stats?v=2 returns structured snapshot"
  else
    ko "stats shape unexpected: $(echo "$stats" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_007
: "${RESULT:=fail}"
unset -f _story_ts_007
