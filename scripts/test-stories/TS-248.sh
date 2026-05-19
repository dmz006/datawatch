#!/usr/bin/env bash
# TS-248 — Schedule lifecycle: add, list, cancel
# tags: surface:cli feature:cli feature:schedules
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-248"
story_preflight "surface:cli feature:cli feature:schedules" || return 0

_story_ts_248() {
  # Get a session ID to schedule against — pick an existing one or create a shell session
  local sess_id
  local sessions_resp
  sessions_resp=$(api GET /api/sessions)
  sess_id=$(echo "$sessions_resp" | python3 -c '
import json,sys
items=json.load(sys.stdin)
if not isinstance(items, list):
    items=items.get("sessions",[])
# prefer a running/waiting_input shell session
for s in items:
    if s.get("backend_family","")=="shell" and s.get("state","") not in ("killed","complete"):
        print(s.get("full_id","") or s.get("id",""))
        break
' 2>/dev/null || echo "")

  local created_session=false
  if [[ -z "$sess_id" ]]; then
    local cr
    cr=$(api POST /api/sessions/start '{"task":"test-sched-fixture-'"$$"'","name":"test-sched-fixture-'"$$"'","backend":"shell","project_dir":"/tmp"}')
    sess_id=$(echo "$cr" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("full_id","") or d.get("id",""))' 2>/dev/null || echo "")
    if [[ -z "$sess_id" ]]; then
      skip "could not find or create a session for scheduling: $cr"
      return
    fi
    add_cleanup sess "$sess_id"
    created_session=true
  fi

  # Add a schedule far in the future
  local sched_resp sched_code sched_body
  sched_resp=$(api_code POST /api/schedules "{\"session_id\":\"$sess_id\",\"command\":\"echo ts248-test\",\"run_at\":\"2099-01-01T00:00:00Z\"}")
  sched_code=$(echo "$sched_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  sched_body=$(echo "$sched_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-248 "add.json" "$sched_body"

  if [[ "$sched_code" == "503" ]]; then
    skip "scheduling not available (503)"
    return
  fi
  if [[ "$sched_code" != "200" && "$sched_code" != "201" ]]; then
    ko "schedule add returned HTTP $sched_code: $sched_body"
    return
  fi

  local sched_id
  sched_id=$(echo "$sched_body" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$sched_id" ]]; then
    ko "schedule add response missing id: $sched_body"
    return
  fi

  # List schedules and verify our entry appears
  local list_resp list_code list_body
  list_resp=$(api_code GET /api/schedules)
  list_code=$(echo "$list_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  list_body=$(echo "$list_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-248 "list.json" "$list_body"

  if [[ "$list_code" != "200" ]]; then
    ko "schedule list returned HTTP $list_code: $list_body"
    return
  fi

  local found
  found=$(echo "$list_body" | python3 -c "
import json,sys
items=json.load(sys.stdin)
print('yes' if isinstance(items,list) and any(s.get('id','')=='$sched_id' for s in items) else 'no')
" 2>/dev/null || echo "no")

  if [[ "$found" != "yes" ]]; then
    ko "schedule $sched_id not found in list: $list_body"
    return
  fi

  # Cancel/delete the schedule: DELETE /api/schedules?id=<id>
  local del_resp del_code del_body
  del_resp=$(api_code DELETE "/api/schedules?id=$sched_id")
  del_code=$(echo "$del_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  del_body=$(echo "$del_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-248 "cancel.json" "$del_body"

  if [[ "$del_code" == "200" ]]; then
    ok "schedule lifecycle: add($sched_id), list(found), cancel(ok)"
  elif [[ "$del_code" == "404" ]]; then
    ko "schedule cancel returned 404 — schedule $sched_id not found for deletion"
  else
    ko "schedule cancel returned HTTP $del_code: $del_body"
  fi
}

RESULT=fail
_story_ts_248
: "${RESULT:=fail}"
unset -f _story_ts_248
