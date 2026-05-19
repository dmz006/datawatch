#!/usr/bin/env bash
# TS-205 — Session hook lifecycle: Start, Activity, and Stop events accepted
# tags: surface:api feature:sessions
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-205"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_205() {
  # Create a shell session
  local cr
  cr=$(api POST /api/sessions/start '{"task":"test-hooks-lifecycle-'"$$"'","name":"test-hooks-lifecycle-'"$$"'","backend":"shell","project_dir":"/tmp"}')
  local sess_id
  sess_id=$(echo "$cr" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("full_id","") or d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$sess_id" ]]; then
    skip "could not create test session: $cr"
    return
  fi
  add_cleanup sess "$sess_id"

  local all_ok=true

  # Send Start hook
  local r c b
  r=$(api_code POST "/api/sessions/$sess_id/hook-event" '{"event":"Start","payload":{}}')
  c=$(echo "$r" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  b=$(echo "$r" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-205 "hook_start.json" "$b"
  [[ "$c" != "200" ]] && all_ok=false

  # Send Activity hook
  r=$(api_code POST "/api/sessions/$sess_id/hook-event" '{"event":"Activity","payload":{}}')
  c=$(echo "$r" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  b=$(echo "$r" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-205 "hook_activity.json" "$b"
  [[ "$c" != "200" ]] && all_ok=false

  # Send Stop hook
  r=$(api_code POST "/api/sessions/$sess_id/hook-event" '{"event":"Stop","payload":{}}')
  c=$(echo "$r" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  b=$(echo "$r" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-205 "hook_stop.json" "$b"
  [[ "$c" != "200" ]] && all_ok=false

  # Kill session
  api POST /api/sessions/kill "{\"id\":\"$sess_id\"}" >/dev/null 2>&1 || true

  if [[ "$all_ok" == "true" ]]; then
    ok "hook lifecycle Start+Activity+Stop all returned HTTP 200"
  else
    ko "one or more hook events failed — see evidence in TS-205/"
  fi
}

RESULT=fail
_story_ts_205
: "${RESULT:=fail}"
unset -f _story_ts_205
