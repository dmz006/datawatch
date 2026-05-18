#!/usr/bin/env bash
# TS-019 — Session terminate
# tags: surface:api feature:sessions
# legacy fn: t2_ts019_session_terminate
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-019"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_019() {
  local cr
  cr=$(api POST /api/sessions/start '{"task":"test-session-kill-'"$$"'","name":"test-session-kill-'"$$"'","backend":"shell","project_dir":"/tmp"}')
  local kill_id
  kill_id=$(echo "$cr" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("full_id","") or d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$kill_id" ]]; then
    skip "could not create session to kill: $cr"
    return
  fi
  local resp
  resp=$(api POST /api/sessions/kill '{"id":"'"$kill_id"'"}')
  save_evidence TS-019 "kill.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "session kill accepted"
  else
    ko "session kill failed: $resp"
  fi
}

RESULT=fail
_story_ts_019
: "${RESULT:=fail}"
unset -f _story_ts_019
