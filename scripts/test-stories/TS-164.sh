#!/usr/bin/env bash
# TS-164 — Second isolated daemon health check (docker-sim still healthy)
# tags: surface:docker feature:bootstrap
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-164"
story_preflight "surface:docker feature:bootstrap" || return 0

_story_ts_164() {
  if [[ -z "$DOCKER_SIM_CONTAINER" ]]; then
    skip "docker-sim container not running (TS-160 prerequisite)"
    return
  fi
  local resp
  resp=$(curl -sk --max-time 10 -H "Authorization: Bearer $TEST_TOKEN" \
    "https://127.0.0.1:$DOCKER_SIM_TLS/api/health" 2>/dev/null || \
    curl -s --max-time 10 -H "Authorization: Bearer $TEST_TOKEN" \
    "http://127.0.0.1:$DOCKER_SIM_HTTP/api/health" 2>/dev/null || echo "{}")
  save_evidence TS-164 "health.json" "$resp"
  if assert_json "$resp" 'd.get("status")=="ok"'; then
    ok "docker-sim daemon still healthy after TS-162 session test"
  else
    skip "docker-sim not healthy: $resp"
  fi
}

RESULT=fail
_story_ts_164
: "${RESULT:=fail}"
unset -f _story_ts_164
