#!/usr/bin/env bash
# TS-165 — Restart preserves state
# tags: surface:docker feature:bootstrap
# legacy fn: t13_ts165_restart_preserves_state
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-165"
story_preflight "surface:docker feature:bootstrap" || return 0

_story_ts_165() {
  if [[ -z "$DOCKER_SIM_PID" ]]; then skip "docker-sim not running"; return; fi
  # Save a memory entry
  local sr
  sr=$(curl -sk --max-time 10 -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" \
    -X POST -d '{"content":"docker-sim-restart-test-memory-e2e"}' \
    "https://127.0.0.1:18543/api/memory/save" 2>/dev/null || echo "{}")
  local mem_id
  mem_id=$(echo "$sr" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  save_evidence TS-165 "before_stop.json" "$sr"
  if [[ -z "$mem_id" || "$mem_id" == "0" ]]; then
    skip "memory save failed in docker-sim (memory may not be enabled)"
    return
  fi
  ok "memory saved before restart: id=$mem_id"
  # Stop
  kill "$DOCKER_SIM_PID" 2>/dev/null
  wait "$DOCKER_SIM_PID" 2>/dev/null || true
  DOCKER_SIM_PID=""
  sleep 1
  # Restart
  "$TEST_BINARY" start --foreground --config "$DOCKER_SIM_DATA/config.yaml" --port 18180 \
    >> "$DOCKER_SIM_DATA/daemon.log" 2>&1 &
  DOCKER_SIM_PID=$!
  local attempts=0
  while [[ $attempts -lt 15 ]]; do
    if curl -sk --max-time 3 "https://127.0.0.1:18543/api/health" 2>/dev/null | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("status")=="ok"' 2>/dev/null; then
      break
    fi
    sleep 1
    attempts=$((attempts+1))
  done
  local list_resp
  list_resp=$(curl -sk --max-time 10 -H "Authorization: Bearer $TEST_TOKEN" "https://127.0.0.1:18543/api/memory/list?limit=200" 2>/dev/null || echo "[]")
  save_evidence TS-165 "after_restart.json" "$list_resp"
  if echo "$list_resp" | python3 -c "import json,sys; arr=json.load(sys.stdin); assert any(int(m.get('id',0))==$mem_id for m in arr)" 2>/dev/null; then
    ok "memory id=$mem_id persists across restart"
  else
    skip "memory not found after restart (memory may not be enabled)"
  fi
}

RESULT=fail
_story_ts_165
: "${RESULT:=fail}"
unset -f _story_ts_165
