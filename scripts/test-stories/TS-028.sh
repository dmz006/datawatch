#!/usr/bin/env bash
# TS-028 — Automaton hard-delete
# tags: surface:api feature:automata
# legacy fn: t3_ts028_automaton_hard_delete
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-028"
story_preflight "surface:api feature:automata" || return 0

_story_ts_028() {
  local p
  p=$(api POST /api/autonomous/prds '{"spec":"test-prd-harddelete-'"$$"'","project_dir":"/tmp","backend":"claude-code","effort":"low"}')
  local del_id
  del_id=$(echo "$p" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$del_id" ]]; then
    skip "Automaton create failed for hard-delete test"
    return
  fi
  local dr
  dr=$(curl "${curl_args[@]}" -X DELETE "$TEST_BASE/api/autonomous/prds/$del_id?hard=true")
  save_evidence TS-028 "delete.json" "$dr"
  if assert_json "$dr" 'd.get("status") == "deleted"'; then
    ok "Automaton hard-delete: status=deleted"
  else
    ko "Automaton hard-delete failed: $dr"
  fi
}

RESULT=fail
_story_ts_028
: "${RESULT:=fail}"
unset -f _story_ts_028
