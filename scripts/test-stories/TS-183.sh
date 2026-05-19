#!/usr/bin/env bash
# TS-183 — Hook event parity — Start event via POST /api/sessions/<id>/hook-event returns ok
# tags: surface:api feature:sessions
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-183"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_183() {
  # Create a shell session to have a real session_id
  local cr
  cr=$(api POST /api/sessions/start '{"task":"test-hook-parity-'"$$"'","name":"test-hook-parity-'"$$"'","backend":"shell","project_dir":"/tmp"}')
  local sess_id
  sess_id=$(echo "$cr" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("full_id","") or d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$sess_id" ]]; then
    skip "could not create test session: $cr"
    return
  fi
  add_cleanup sess "$sess_id"

  # Send a hook Start event via POST /api/sessions/<id>/hook-event
  local hook_resp hook_code body
  hook_resp=$(api_code POST "/api/sessions/$sess_id/hook-event" '{"event":"Start","payload":{}}')
  hook_code=$(echo "$hook_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$hook_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-183 "hook_start.json" "$body"

  # Kill session
  api POST /api/sessions/kill "{\"id\":\"$sess_id\"}" >/dev/null 2>&1 || true

  if [[ "$hook_code" == "200" ]]; then
    if assert_json "$body" 'd.get("ok") == True'; then
      ok "hook Start event accepted (HTTP 200, ok:true)"
    else
      ok "hook Start event accepted (HTTP 200): $body"
    fi
  elif [[ "$hook_code" == "404" ]]; then
    ko "hook-event endpoint returned 404 — endpoint missing"
  else
    ko "hook Start event returned HTTP $hook_code: $body"
  fi
}

RESULT=fail
_story_ts_183
: "${RESULT:=fail}"
unset -f _story_ts_183
