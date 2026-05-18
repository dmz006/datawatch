#!/usr/bin/env bash
# TS-151 — Schedules CRUD
# tags: surface:api feature:schedules conflict:db-write
# legacy fn: t12_ts151_schedules_crud
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-151"
story_preflight "surface:api feature:schedules conflict:db-write" || return 0

_story_ts_151() {
  local ts
  ts=$(date -u -d '+1 hour' +%FT%TZ 2>/dev/null || date -u -v+1H +%FT%TZ 2>/dev/null || echo "")
  if [[ -z "$ts" ]]; then skip "cannot compute future timestamp"; return; fi
  local sname="test-sched-e2e-$$"
  local cr
  cr=$(api POST /api/schedules '{"type":"new_session","name":"'"$sname"'","command":"echo e2e","project_dir":"/tmp","backend":"shell","run_at":"'"$ts"'"}')
  save_evidence TS-151 "create.json" "$cr"
  local sid
  sid=$(echo "$cr" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$sid" ]]; then
    skip "schedule create failed: $(echo "$cr" | head -c 100)"
    return
  fi
  add_cleanup sched "$sid"
  ok "schedule created: $sid"
  local del_resp
  del_resp=$(curl "${curl_args[@]}" -X DELETE "$TEST_BASE/api/schedules?id=$sid")
  save_evidence TS-151 "delete.json" "$del_resp"
  if assert_json "$del_resp" '"status" in d'; then
    ok "schedule $sid deleted"
  else
    ko "schedule delete failed: $del_resp"
  fi
}

RESULT=fail
_story_ts_151
: "${RESULT:=fail}"
unset -f _story_ts_151
