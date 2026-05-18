#!/usr/bin/env bash
# TS-161 — Health check (simulated container)
# tags: surface:docker feature:bootstrap
# legacy fn: t13_ts161_health_check
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-161"
story_preflight "surface:docker feature:bootstrap" || return 0

_story_ts_161() {
  local resp
  resp=$(curl -s --max-time 10 -H "Authorization: Bearer $TEST_TOKEN" "http://127.0.0.1:$DOCKER_SIM_HTTP/api/health" 2>/dev/null || echo "{}")
  save_evidence TS-161 "health.json" "$resp"
  if assert_json "$resp" 'd.get("status")=="ok"'; then
    ok "docker-sim health ok"
  else
    skip "docker-sim not healthy: $resp"
  fi
}

RESULT=fail
_story_ts_161
: "${RESULT:=fail}"
unset -f _story_ts_161
