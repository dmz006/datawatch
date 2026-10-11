#!/usr/bin/env bash
# TS-788 — BL406 Phase 1: ProjectRulesScanner content rule fires a real
# finding against a real file on disk (not a mock). Pins v9.0.12's
# "fourth scan category now actually evaluates operator-defined rules".
# Mutates the GLOBAL autonomous.scan.project_rules list via the
# dedicated PUT /api/autonomous/scan/config endpoint (full-replace, per
# that endpoint's own documented contract) — must not run concurrently
# with any other story that also depends on that list, hence
# conflict:selfconfig and no parallel:ok (serial lane only).
# tags: surface:api feature:automata conflict:selfconfig
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-788"
story_preflight "surface:api feature:automata conflict:selfconfig" || return 0

_story_ts_788() {
  if [[ "$(t3_check_autonomous)" != "yes" ]]; then skip "autonomous disabled"; return; fi

  local scan_dir before_rules
  scan_dir=$(mktemp -d /tmp/dw-ts788-XXXXXX)
  echo "normal line" > "$scan_dir/notes.txt"
  echo "TODO-TS788-MARKER needs fixing" >> "$scan_dir/notes.txt"

  before_rules=$(api GET /api/autonomous/scan/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print(json.dumps(d.get("project_rules",[])))' 2>/dev/null || echo "[]")

  local put_resp
  put_resp=$(api PUT /api/autonomous/scan/config '{"project_rules":[{"id":"ts788-content","name":"TS-788 content fixture rule","type":"content","granularity":"task_complete","pattern":"*.txt|TODO-TS788-MARKER","severity":"warning"}]}')
  save_evidence TS-788 "0_put_rule.json" "$put_resp"

  local atm atm_id
  atm=$(api POST /api/autonomous/prds "{\"spec\":\"TS-788 project-rules content scan fixture\",\"project_dir\":\"$scan_dir\",\"effort\":\"low\"}")
  atm_id=$(echo "$atm" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -n "$atm_id" ]]; then add_cleanup automaton "$atm_id"; fi

  local found=0
  if [[ -n "$atm_id" ]]; then
    local scan_resp
    scan_resp=$(api POST "/api/autonomous/prds/$atm_id/scan" '{}')
    save_evidence TS-788 "1_scan.json" "$scan_resp"
    found=$(echo "$scan_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);fs=d.get("findings",[]);print(1 if any(f.get("rule_id")=="ts788-content" for f in fs) else 0)' 2>/dev/null || echo 0)
  fi

  # Restore the original rule list and clean up the scratch dir before
  # asserting, so a failed assertion still leaves everything clean.
  api PUT /api/autonomous/scan/config "{\"project_rules\":$before_rules}" >/dev/null
  rm -rf "$scan_dir"

  if [[ -z "$atm_id" ]]; then
    ko "could not create fixture automaton: $(echo "$atm" | head -c 200)"
  elif [[ "$found" == "1" ]]; then
    ok "POST .../scan reports a real finding for the configured content rule against a file on disk (BL406 Phase 1, v9.0.12)"
  else
    ko "expected a finding with rule_id=ts788-content, got none: $(echo "$scan_resp" | head -c 300)"
  fi
}

RESULT=fail
_story_ts_788
: "${RESULT:=fail}"
unset -f _story_ts_788
