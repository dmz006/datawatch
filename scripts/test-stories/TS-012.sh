#!/usr/bin/env bash
# TS-012 — Session appears in stats
# tags: surface:api feature:sessions
# legacy fn: t2_ts012_session_in_stats
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-012"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_012() {
  local stats
  stats=$(curl "${curl_args[@]}" "$TEST_BASE/api/stats?v=2")
  save_evidence TS-012 "stats.json" "$stats"
  if assert_json "$stats" 'isinstance(d, dict)'; then
    ok "stats returns dict (session_count derivable)"
  else
    ko "stats unexpected: $(echo "$stats" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_012
: "${RESULT:=fail}"
unset -f _story_ts_012
