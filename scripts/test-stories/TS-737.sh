#!/usr/bin/env bash
# TS-737 — PRD reset_task 400 when PRD is not in running/failed state
# tags: surface:api feature:automata parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-737"
story_preflight "surface:api feature:automata" || return 0

_story_ts_737() {
  local a_ok
  a_ok=$(api GET /api/autonomous/config \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$a_ok" != "yes" ]]; then
    skip "autonomous disabled"
    return
  fi

  # Create a PRD in draft/approved state (not running)
  local prd_id
  prd_id=$(api POST /api/autonomous/prds \
    '{"spec":"TS-737 reset_task state guard test","project_dir":"/tmp/ts737"}' \
    | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$prd_id" ]]; then
    skip "could not create PRD"
    return
  fi

  _cleanup() { api DELETE "/api/autonomous/prds/$prd_id" >/dev/null 2>&1 || true; }

  # PRD is in draft state — reset_task should fail
  local code
  code=$(api_code POST "/api/autonomous/prds/$prd_id/reset_task" \
    '{"task_id":"task-does-not-exist","actor":"ts737","force":false}' \
    | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')

  _cleanup

  if [[ "$code" == "400" || "$code" == "409" ]]; then
    ok "reset_task on non-running PRD returns HTTP $code (enforces running/failed guard)"
    return
  fi
  if [[ "$code" == "404" ]]; then
    skip "reset_task endpoint not found (404)"
    return
  fi

  ko "reset_task on non-running PRD returned HTTP $code; want 400 or 409"
}

RESULT=fail
_story_ts_737
: "${RESULT:=fail}"
unset -f _story_ts_737
unset -f _cleanup 2>/dev/null || true
