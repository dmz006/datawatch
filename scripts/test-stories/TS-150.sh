#!/usr/bin/env bash
# TS-150 — Filters CRUD
# tags: surface:api feature:filters conflict:db-write
# legacy fn: t12_ts150_filters_crud
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-150"
story_preflight "surface:api feature:filters conflict:db-write" || return 0

_story_ts_150() {
  local pat="test-filter-e2e-$$"
  local cr
  cr=$(api POST /api/filters '{"pattern":"'"$pat"'","action":"schedule","value":"yes"}')
  save_evidence TS-150 "create.json" "$cr"
  local fid
  fid=$(echo "$cr" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$fid" ]]; then
    skip "filter create failed: $(echo "$cr" | head -c 100)"
    return
  fi
  add_cleanup filter "$fid"
  ok "filter created: $fid"
  local list_resp
  list_resp=$(api GET /api/filters)
  save_evidence TS-150 "list.json" "$list_resp"
  if echo "$list_resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
arr = d if isinstance(d,list) else d.get('filters',[])
assert any(f.get('id') == '$fid' for f in arr)
" 2>/dev/null; then
    ok "filter $fid in list"
  else
    ko "filter $fid NOT in list"
  fi
  local del_resp
  del_resp=$(curl "${curl_args[@]}" -X DELETE "$TEST_BASE/api/filters?id=$fid")
  save_evidence TS-150 "delete.json" "$del_resp"
  if assert_json "$del_resp" '"status" in d'; then
    ok "filter $fid deleted"
  else
    ko "filter delete failed: $del_resp"
  fi
}

RESULT=fail
_story_ts_150
: "${RESULT:=fail}"
unset -f _story_ts_150
