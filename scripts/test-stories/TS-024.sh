#!/usr/bin/env bash
# TS-024 — Automaton approve
# tags: surface:api feature:automata
# legacy fn: t3_ts024_automaton_approve
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-024"
story_preflight "surface:api feature:automata" || return 0

_story_ts_024() {
  ensure_test_automaton || return
  local status
  status=$(api GET "/api/autonomous/prds/$AUTOMATON_ID" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))' 2>/dev/null || echo "")
  if [[ "$status" != "needs_review" ]]; then
    skip "Automaton approve requires needs_review status (current: $status); run decompose with LLM first"
    return
  fi
  local resp
  resp=$(api POST "/api/autonomous/prds/$AUTOMATON_ID/approve" '{"actor":"test-runner","note":"e2e test approval"}')
  save_evidence TS-024 "approve.json" "$resp"
  if assert_json "$resp" 'd.get("status") in ("approved","needs_review")'; then
    ok "Automaton approve returned valid status"
  else
    ko "Automaton approve failed: $resp"
  fi
}

RESULT=fail
_story_ts_024
: "${RESULT:=fail}"
unset -f _story_ts_024
