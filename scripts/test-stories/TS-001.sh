#!/usr/bin/env bash
# TS-001 — Fresh daemon starts on test ports
# tags: surface:api feature:bootstrap blocking
# legacy fn: t1_ts001_fresh_start
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-001"
story_preflight "surface:api feature:bootstrap blocking" || return 0

_story_ts_001() {
  local health
  health=$(curl -sk --max-time 10 "$TEST_BASE/api/health" 2>/dev/null || echo "{}")
  save_evidence TS-001 "health.json" "$health"
  if assert_json "$health" 'd.get("status")=="ok"'; then
    ok "daemon started, health ok"
  else
    ko "daemon not healthy: $health"
  fi
}

RESULT=fail
_story_ts_001
: "${RESULT:=fail}"
unset -f _story_ts_001
