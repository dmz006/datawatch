#!/usr/bin/env bash
# TS-787 — BL406 Phase 0: rules_file/context_file/upstream_repos config round-trip
# Pins v9.0.11's "Full parity: GET/PUT /api/config round-trips the 3 new
# fields" claim — writes real (non-default) values, reads them back from
# a FRESH GET (not the PUT response), then restores whatever was there
# before so this doesn't leave the daemon's real project-rules config
# mutated for any other story that runs after it.
# tags: surface:api feature:automata conflict:selfconfig
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-787"
story_preflight "surface:api feature:automata conflict:selfconfig" || return 0

_story_ts_787() {
  if [[ "$(t3_check_autonomous)" != "yes" ]]; then skip "autonomous disabled"; return; fi

  local before
  before=$(api GET /api/config)
  save_evidence TS-787 "0_before.json" "$before"
  local rf_before cf_before
  rf_before=$(echo "$before" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("autonomous",{}).get("rules_file",""))' 2>/dev/null || echo "")
  cf_before=$(echo "$before" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("autonomous",{}).get("context_file",""))' 2>/dev/null || echo "")

  local put_resp
  put_resp=$(api PUT /api/config '{"autonomous.rules_file":"TS787-RULES.md","autonomous.context_file":"TS787-CONTEXT.md","autonomous.upstream_repos":[{"name":"ts787-app","owner_repo":"dmz006/ts787-fixture"}]}')
  save_evidence TS-787 "1_put.json" "$put_resp"

  local after rf_after cf_after up_after
  after=$(api GET /api/config)
  save_evidence TS-787 "2_after.json" "$after"
  rf_after=$(echo "$after" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("autonomous",{}).get("rules_file",""))' 2>/dev/null || echo "")
  cf_after=$(echo "$after" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("autonomous",{}).get("context_file",""))' 2>/dev/null || echo "")
  up_after=$(echo "$after" | python3 -c 'import json,sys;d=json.load(sys.stdin);u=d.get("autonomous",{}).get("upstream_repos",[]);print(u[0].get("owner_repo","") if u else "")' 2>/dev/null || echo "")

  # Restore before asserting, so a failed assertion still leaves the
  # daemon's real config clean for whatever story runs next.
  api PUT /api/config "{\"autonomous.rules_file\":\"$rf_before\",\"autonomous.context_file\":\"$cf_before\",\"autonomous.upstream_repos\":[]}" >/dev/null

  if [[ "$rf_after" == "TS787-RULES.md" && "$cf_after" == "TS787-CONTEXT.md" && "$up_after" == "dmz006/ts787-fixture" ]]; then
    ok "PUT /api/config round-trips autonomous.rules_file/context_file/upstream_repos (BL406 Phase 0, v9.0.11)"
  else
    ko "round-trip mismatch: rules_file='$rf_after' context_file='$cf_after' upstream_repos[0].owner_repo='$up_after'"
  fi
}

RESULT=fail
_story_ts_787
: "${RESULT:=fail}"
unset -f _story_ts_787
