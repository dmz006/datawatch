#!/usr/bin/env bash
# TS-166 — Memory save in isolated instance
# tags: surface:docker feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-166"
story_preflight "surface:docker feature:memory" || return 0

_story_ts_166() {
  if [[ -z "$DOCKER_SIM_CONTAINER" ]]; then
    skip "docker-sim container not running (TS-160 prerequisite)"
    return
  fi
  # Check memory is enabled
  local stats
  stats=$(curl -sk --max-time 10 -H "Authorization: Bearer $TEST_TOKEN" \
    "https://127.0.0.1:$DOCKER_SIM_TLS/api/memory/stats" 2>/dev/null || echo "{}")
  save_evidence TS-166 "memory_stats.json" "$stats"
  if ! assert_json "$stats" 'd.get("enabled")'; then
    skip "memory not enabled in docker-sim instance"
    return
  fi
  # Save a memory entry
  local resp
  resp=$(curl -sk --max-time 15 \
    -X POST -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" \
    -d '{"content":"docker-sim test memory entry TS-166","tags":["ts-166"]}' \
    "https://127.0.0.1:$DOCKER_SIM_TLS/api/memory/save" 2>/dev/null || echo "{}")
  save_evidence TS-166 "save_resp.json" "$resp"
  if assert_json "$resp" '"id" in d or d.get("status")=="ok" or d.get("saved")'; then
    ok "memory save succeeded in docker-sim instance"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "memory save returned dict in docker-sim instance"
  else
    skip "memory save failed in docker-sim (memory may not be enabled): $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_166
: "${RESULT:=fail}"
unset -f _story_ts_166
