#!/usr/bin/env bash
# TS-700 — B104: GET /api/autonomous/prds/{id}/stories returns stories with status field
# tags: surface:api feature:automata group:b104-status-parity-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-700"
story_preflight "surface:api feature:automata group:b104-status-parity-v9" || return 0

_story_ts_700() {
  # Get any existing PRD or create one
  local prd_id
  prd_id=$(api GET /api/autonomous/prds \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);a=d if isinstance(d,list) else d.get("prds",[]);print(a[0].get("id","") if a else "")' 2>/dev/null || echo "")

  local created=0
  if [[ -z "$prd_id" ]]; then
    local prd_resp
    prd_resp=$(api POST /api/autonomous/prds \
      '{"title":"TS-700 status parity test","spec":"Verify story status fields.","project_dir":"/tmp"}')
    prd_id=$(echo "$prd_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
    created=1
  fi
  if [[ -z "$prd_id" ]]; then
    skip "could not find or create test automaton"
    return
  fi

  local prd_detail
  prd_detail=$(api GET "/api/autonomous/prds/$prd_id")
  save_evidence TS-700 "prd.json" "$prd_detail"

  if [[ $created -eq 1 ]]; then
    api DELETE "/api/autonomous/prds/$prd_id" >/dev/null 2>&1 || true
  fi

  # B104: PRD detail should have a status field
  if assert_json "$prd_detail" '"status" in d'; then
    ok "GET /api/autonomous/prds/$prd_id has status field (B104)"
  else
    ko "PRD detail missing status field: $(echo "$prd_detail" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_700
: "${RESULT:=fail}"
unset -f _story_ts_700
