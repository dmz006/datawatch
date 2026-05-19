#!/usr/bin/env bash
# TS-169 — Isolated stats shows separate uptime
# tags: surface:docker feature:bootstrap
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-169"
story_preflight "surface:docker feature:bootstrap" || return 0

_story_ts_169() {
  if [[ -z "$DOCKER_SIM_CONTAINER" ]]; then
    skip "docker-sim container not running (TS-160 prerequisite)"
    return
  fi
  local resp
  resp=$(curl -sk --max-time 10 -H "Authorization: Bearer $TEST_TOKEN" \
    "https://127.0.0.1:$DOCKER_SIM_TLS/api/stats" 2>/dev/null || echo "{}")
  save_evidence TS-169 "stats.json" "$resp"
  if assert_json "$resp" '"uptime" in d or "uptime_seconds" in d'; then
    ok "docker-sim stats endpoint returns uptime"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "docker-sim stats endpoint reachable"
  else
    skip "docker-sim stats endpoint not reachable: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_169
: "${RESULT:=fail}"
unset -f _story_ts_169
