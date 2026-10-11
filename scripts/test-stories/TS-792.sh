#!/usr/bin/env bash
# TS-792 — BL406 Phase 5 (B111): set_guided_mode actually gates now.
# Pins v9.0.17's fix: toggling guided_mode:true must set
# guided_mode_source="operator" live (not just after a daemon restart
# re-reads migrateGuidedMode), and guided_mode:false must clear it.
# Per-PRD field only — no global config touched, safe to run parallel.
# tags: surface:api feature:automata parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-792"
story_preflight "surface:api feature:automata parallel:ok" || return 0

_story_ts_792() {
  if [[ "$(t3_check_autonomous)" != "yes" ]]; then skip "autonomous disabled"; return; fi

  local atm atm_id
  atm=$(api POST /api/autonomous/prds '{"spec":"TS-792 guided-mode fixture","project_dir":"/tmp","effort":"low"}')
  atm_id=$(echo "$atm" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$atm_id" ]]; then
    ko "could not create fixture automaton: $(echo "$atm" | head -c 200)"
    return
  fi
  add_cleanup automaton "$atm_id"

  local on_resp on_source
  on_resp=$(api POST "/api/autonomous/prds/$atm_id/set_guided_mode" '{"guided_mode":true}')
  save_evidence TS-792 "0_on.json" "$on_resp"
  on_source=$(echo "$on_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("guided_mode_source",""))' 2>/dev/null || echo "")

  local off_resp off_source off_mode
  off_resp=$(api POST "/api/autonomous/prds/$atm_id/set_guided_mode" '{"guided_mode":false}')
  save_evidence TS-792 "1_off.json" "$off_resp"
  off_source=$(echo "$off_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("guided_mode_source",""))' 2>/dev/null || echo "")
  off_mode=$(echo "$off_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("guided_mode",False))' 2>/dev/null || echo "")

  if [[ "$on_source" == "operator" && -z "$off_source" && "$off_mode" == "False" ]]; then
    ok "set_guided_mode true/false live-applies guided_mode_source operator/cleared (B111, v9.0.17)"
  else
    ko "guided_mode_source mismatch: on='$on_source' (want operator), off='$off_source' (want empty), off guided_mode=$off_mode (want False)"
  fi
}

RESULT=fail
_story_ts_792
: "${RESULT:=fail}"
unset -f _story_ts_792
