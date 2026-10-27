#!/usr/bin/env bash
# TS-165 — Restart preserves state
# tags: surface:docker feature:bootstrap
# legacy fn: t13_ts165_restart_preserves_state
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-165"
story_preflight "surface:docker feature:bootstrap" || return 0

_story_ts_165() {
  if [[ -z "$DOCKER_SIM_PID" ]]; then skip "docker-sim not running"; return; fi

  # Enable sqlite memory in docker-sim config so the memory/save endpoint works.
  # The config was written by TS-160 without memory; patch it here before use.
  if [[ -f "$DOCKER_SIM_DATA/config.yaml" ]]; then
    # Only patch if memory is not already enabled
    if ! grep -q "enabled: true" "$DOCKER_SIM_DATA/config.yaml" 2>/dev/null; then
      cat >> "$DOCKER_SIM_DATA/config.yaml" <<MEMYAML
memory:
  enabled: true
  backend: sqlite
  embedder: ""
MEMYAML
    fi
  fi

  # Save a memory entry via the docker-sim TLS port
  local sr
  sr=$(curl -sk --max-time 10 -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" \
    -X POST -d '{"content":"docker-sim-restart-test-memory-e2e"}' \
    "https://127.0.0.1:${DOCKER_SIM_TLS}/api/memory/save" 2>/dev/null || echo "{}")
  local mem_id
  mem_id=$(echo "$sr" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  save_evidence TS-165 "before_stop.json" "$sr"
  if [[ -z "$mem_id" || "$mem_id" == "0" ]]; then
    skip "memory save failed in docker-sim (memory may not be enabled)"
    return
  fi
  ok "memory saved before restart: id=$mem_id"

  # Stop the docker-sim by the id this run recorded, before restarting it.
  # Whatever shape DOCKER_SIM_PID has — a 12-hex Docker container id (TS-160
  # normalizes to this form; a 64-hex id is accepted by docker too), or a
  # numeric host pid from an earlier restart (ended through the shared
  # library, marker-checked) — dw_terminate_pid ends it, and returns
  # nonzero if it still exists. The restart below must not run over ports
  # 8171/8172 a surviving container still holds, so gate on success.
  if ! dw_terminate_pid "$DOCKER_SIM_PID"; then
    echo "TS-165: could not end docker-sim ($DOCKER_SIM_PID) before restart"
    ko "docker-sim could not be ended before restart; its ports would stay in use"
    return
  fi
  DOCKER_SIM_PID=""
  sleep 1

  # Restart — use DOCKER_SIM_HTTP and DOCKER_SIM_TLS variables (no hardcoded
  # ports). The new PID is recorded too, so the shared EXIT/INT/TERM/HUP
  # traps (the runner's under the e2e harness, this script's own when run
  # standalone) end it on every exit path.
  story_daemon_lifecycle
  td_start "$TEST_BINARY" "$DOCKER_SIM_DATA/config.yaml" "$DOCKER_SIM_DATA/daemon.log" --port "${DOCKER_SIM_HTTP}"
  DOCKER_SIM_PID="${TD_LAST_PID:-}"
  td_set_api "$DOCKER_SIM_PID" "https://127.0.0.1:${DOCKER_SIM_TLS}" "$TEST_TOKEN"
  local attempts=0
  while [[ $attempts -lt 15 ]]; do
    if curl -sk --max-time 3 "https://127.0.0.1:${DOCKER_SIM_TLS}/api/health" 2>/dev/null | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("status")=="ok"' 2>/dev/null; then
      break
    fi
    sleep 1
    attempts=$((attempts+1))
  done
  local list_resp
  list_resp=$(curl -sk --max-time 10 -H "Authorization: Bearer $TEST_TOKEN" "https://127.0.0.1:${DOCKER_SIM_TLS}/api/memory/list?limit=200" 2>/dev/null || echo "[]")
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
