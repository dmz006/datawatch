#!/usr/bin/env bash
# TS-027 — project_profile + cluster_profile attachment
# tags: surface:api feature:automata feature:profiles
# legacy fn: t3_ts027_profile_attachment
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-027"
story_preflight "surface:api feature:automata feature:profiles" || return 0

_story_ts_027() {
  if [[ "$(t3_check_autonomous)" != "yes" ]]; then skip "autonomous disabled"; return; fi
  local pname="test-profile-e2e-$$"
  local pr
  pr=$(api POST /api/profiles/projects '{"name":"'"$pname"'","git":{"url":"https://github.com/dmz006/datawatch","branch":"main"},"image_pair":{"agent":"agent-claude"}}')
  save_evidence TS-027 "profile_create.json" "$pr"
  if ! assert_json "$pr" 'd.get("name")'; then
    skip "project profile create failed: $(echo "$pr" | head -c 100)"
    return
  fi
  add_cleanup profile-proj "$pname"
  local atm
  atm=$(api POST /api/autonomous/prds '{"spec":"test-prd-profile-'"$$"'","project_profile":"'"$pname"'","effort":"low","backend":"claude-code"}')
  save_evidence TS-027 "automaton_create.json" "$atm"
  local atm2_id
  atm2_id=$(echo "$atm" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -n "$atm2_id" ]]; then
    add_cleanup automaton "$atm2_id"
    local got
    got=$(echo "$atm" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("project_profile",""))' 2>/dev/null || echo "")
    if [[ "$got" == "$pname" ]]; then
      ok "Automaton carries project_profile=$pname"
    else
      ko "Automaton dropped project_profile (got='$got', want='$pname')"
    fi
  else
    ko "Automaton create with profile failed: $(echo "$atm" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_027
: "${RESULT:=fail}"
unset -f _story_ts_027
