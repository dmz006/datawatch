#!/usr/bin/env bash
# TS-025 — Automaton run → spawn (SKIP if LLM unreachable)
# tags: surface:api feature:automata conflict:llm
# legacy fn: t3_ts025_automaton_run
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-025"
story_preflight "surface:api feature:automata conflict:llm" || return 0

_story_ts_025() {
  ensure_test_automaton || return
  local avail
  avail=$(wait_for_llm_backend 3 15)
  if [[ -z "$avail" ]]; then skip "no LLM backend available+enabled after retries"; return; fi
  local status
  status=$(api GET "/api/autonomous/prds/$AUTOMATON_ID" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))' 2>/dev/null || echo "")
  if [[ "$status" != "approved" ]]; then
    skip "Automaton run requires approved status (current: $status); approve first"
    return
  fi
  local resp
  resp=$(api POST "/api/autonomous/prds/$AUTOMATON_ID/run" '{}')
  save_evidence TS-025 "run.json" "$resp"
  if assert_json "$resp" '"status" in d'; then
    ok "Automaton run accepted: $(echo "$resp" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status","?"))' 2>/dev/null)"
    # Cancel to avoid background work
    curl "${curl_args[@]}" -X DELETE "$TEST_BASE/api/autonomous/prds/$AUTOMATON_ID" >/dev/null 2>&1
  else
    ko "Automaton run failed: $resp"
  fi
}

RESULT=fail
_story_ts_025
: "${RESULT:=fail}"
unset -f _story_ts_025
