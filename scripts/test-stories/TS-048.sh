#!/usr/bin/env bash
# TS-048 — Memory stats endpoint
# tags: surface:api feature:memory
# legacy fn: t5_ts048_memory_stats
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-048"
story_preflight "surface:api feature:memory" || return 0

_story_ts_048() {
  local resp
  resp=$(api GET /api/memory/stats)
  save_evidence TS-048 "stats.json" "$resp"
  if assert_json "$resp" '"enabled" in d'; then
    ok "GET /api/memory/stats has enabled field"
  else
    ko "memory stats shape wrong: $resp"
  fi
}

RESULT=fail
_story_ts_048
: "${RESULT:=fail}"
unset -f _story_ts_048
