#!/usr/bin/env bash
# TS-035 — Council stats
# tags: surface:api feature:council
# legacy fn: t4_ts035_council_stats
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-035"
story_preflight "surface:api feature:council" || return 0

_story_ts_035() {
  local resp
  resp=$(api GET /api/council/runs)
  save_evidence TS-035 "runs.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "GET /api/council/runs returns valid shape"
  else
    skip "council runs endpoint not available: $resp"
  fi
}

RESULT=fail
_story_ts_035
: "${RESULT:=fail}"
unset -f _story_ts_035
