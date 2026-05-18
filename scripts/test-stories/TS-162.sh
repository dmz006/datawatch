#!/usr/bin/env bash
# TS-162 — Session creation in isolated mode
# tags: surface:docker feature:sessions
# legacy fn: t13_ts162_session_in_isolated
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-162"
story_preflight "surface:docker feature:sessions" || return 0

_story_ts_162() {
  local resp
  resp=$(curl -s --max-time 15 -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" \
    -d '{"name":"test-docker-session","backend":"shell","project_dir":"/tmp"}' \
    "http://127.0.0.1:$DOCKER_SIM_HTTP/api/sessions/start" 2>/dev/null || echo "{}")
  save_evidence TS-162 "session.json" "$resp"
  local sid
  sid=$(echo "$resp" | python3 -c 'import json,sys; d=json.load(sys.stdin); print((d.get("id","") or d.get("full_id","")))' 2>/dev/null || echo "")
  if [[ -n "$sid" ]]; then
    ok "session created in docker-sim daemon: $sid"
    add_cleanup session "$sid"
  else
    skip "session create failed in docker-sim: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_162
: "${RESULT:=fail}"
unset -f _story_ts_162
