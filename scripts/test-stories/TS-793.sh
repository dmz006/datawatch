#!/usr/bin/env bash
# TS-793 — BL406 Phase 5 (B112): opt-in scope_drift rule fires a real
# finding when a task's spec contains code-creation language but the
# parent PRD spec declares doc-only/no-code-changes work. Opt-in (not
# in scan.DefaultConfig()) — mutates the GLOBAL
# autonomous.scan.project_rules list via the dedicated scan/config
# endpoint, same restore discipline as TS-788.
#
# add_story/add_task are rejected while a PRD is still "draft" (found
# live while writing TS-789) — the only REST-reachable way to
# needs_review is a real decompose, so this needs conflict:llm too.
# Once there, add_story/add_task give deterministic content for the
# actual assertion rather than depending on the LLM's own output.
# tags: surface:api feature:automata conflict:llm conflict:selfconfig
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-793"
story_preflight "surface:api feature:automata conflict:llm conflict:selfconfig" || return 0

_story_ts_793() {
  if [[ "$(t3_check_autonomous)" != "yes" ]]; then skip "autonomous disabled"; return; fi
  local avail
  avail=$(wait_for_llm_backend 3 15)
  if [[ -z "$avail" ]]; then skip "no LLM backend available+enabled after retries"; return; fi

  local before_rules
  before_rules=$(api GET /api/autonomous/scan/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print(json.dumps(d.get("project_rules",[])))' 2>/dev/null || echo "[]")

  local put_resp
  put_resp=$(api PUT /api/autonomous/scan/config '{"project_rules":[{"id":"ts793-scope-drift","name":"TS-793 scope-drift fixture rule","type":"scope_drift","granularity":"task_complete","severity":"warning"}]}')
  save_evidence TS-793 "0_put_rule.json" "$put_resp"

  local atm atm_id
  atm=$(api POST /api/autonomous/prds '{"spec":"TS-793 scope-drift fixture plan. This PRD is documentation only -- no code changes.","project_dir":"/tmp","effort":"low","backend":"ollama"}')
  atm_id=$(echo "$atm" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -n "$atm_id" ]]; then add_cleanup automaton "$atm_id"; fi

  local found=0 scan_resp story_id status=""
  if [[ -n "$atm_id" ]]; then
    curl "${curl_args[@]}" --max-time 300 -X POST "$TEST_BASE/api/autonomous/prds/$atm_id/decompose" -o /dev/null -w "" || true
    for i in $(seq 1 60); do
      status=$(api GET "/api/autonomous/prds/$atm_id" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))' 2>/dev/null || echo "")
      [[ "$status" == "needs_review" || "$status" == "approved" ]] && break
      sleep 2
    done
  fi

  if [[ "$status" == "needs_review" ]]; then
    local story_resp
    story_resp=$(api POST "/api/autonomous/prds/$atm_id/add_story" '{"title":"TS-793 story","description":"Update the docs."}')
    story_id=$(echo "$story_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);ss=d.get("stories",[]) or d.get("story",[]);print(ss[-1].get("id","") if ss else "")' 2>/dev/null || echo "")
    save_evidence TS-793 "1_add_story.json" "$story_resp"
    if [[ -n "$story_id" ]]; then
      api POST "/api/autonomous/prds/$atm_id/add_task" "{\"story_id\":\"$story_id\",\"title\":\"TS-793 task\",\"spec\":\"Implement the widget in Go.\"}" >/dev/null
    fi
    scan_resp=$(api POST "/api/autonomous/prds/$atm_id/scan" '{}')
    save_evidence TS-793 "2_scan.json" "$scan_resp"
    found=$(echo "$scan_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);fs=d.get("findings",[]);print(1 if any(f.get("rule_id")=="ts793-scope-drift" for f in fs) else 0)' 2>/dev/null || echo 0)
  fi

  api PUT /api/autonomous/scan/config "{\"project_rules\":$before_rules}" >/dev/null

  if [[ "$status" != "needs_review" ]]; then
    skip "could not reach needs_review via decompose (status=$status, LLM may be unreachable in test env)"
  elif [[ -z "$story_id" ]]; then
    ko "add_story succeeded reaching needs_review but returned no story id: $(echo "$story_resp" | head -c 200)"
  elif [[ "$found" == "1" ]]; then
    ok "opt-in scope_drift rule flags a code-creation task under a doc-only PRD (BL406 Phase 5, B112, v9.0.17)"
  else
    ko "expected a scope_drift finding, got none: $(echo "$scan_resp" | head -c 300)"
  fi
}

RESULT=fail
_story_ts_793
: "${RESULT:=fail}"
unset -f _story_ts_793
