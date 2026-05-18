#!/usr/bin/env bash
# TS-002 — Health endpoint shape
# tags: surface:api feature:bootstrap blocking
# legacy fn: t1_ts002_health_shape
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-002"
story_preflight "surface:api feature:bootstrap blocking" || return 0

_story_ts_002() {
  local health
  health=$(api GET /api/health)
  save_evidence TS-002 "health.json" "$health"
  if assert_json "$health" '"status" in d and "version" in d'; then
    ok "health shape: status+version present"
  else
    ko "health shape wrong: $health"
  fi
}

RESULT=fail
_story_ts_002
: "${RESULT:=fail}"
unset -f _story_ts_002
