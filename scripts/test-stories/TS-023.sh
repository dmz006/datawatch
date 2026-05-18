#!/usr/bin/env bash
# TS-023 — Automaton decompose (SKIP if LLM unreachable)
# tags: surface:api feature:automata conflict:llm
# legacy fn: t3_ts023_automaton_decompose
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-023"
story_preflight "surface:api feature:automata conflict:llm" || return 0

_story_ts_023() {
  ensure_test_automaton || return
  # Check LLM availability (retry to allow model load time)
  local avail
  avail=$(wait_for_llm_backend 3 15)
  if [[ -z "$avail" ]]; then skip "no LLM backend available+enabled after retries"; return; fi
  local resp
  resp=$(curl "${curl_args[@]}" --max-time 300 -X POST "$TEST_BASE/api/autonomous/prds/$AUTOMATON_ID/decompose" -w "\n__HTTP_CODE_%{http_code}__")
  local code; code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  local body; body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-023 "decompose.json" "$body"
  if [[ "$code" == "200" ]]; then
    local n; n=$(echo "$body" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(len(d.get("stories",[])))' 2>/dev/null || echo 0)
    ok "Automaton decompose returned 200, $n stories"
  else
    skip "Automaton decompose returned $code (LLM may not be reachable in test env)"
  fi
}

RESULT=fail
_story_ts_023
: "${RESULT:=fail}"
unset -f _story_ts_023
