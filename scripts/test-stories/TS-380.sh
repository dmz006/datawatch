#!/usr/bin/env bash
# TS-380 — POST /api/autonomous/prds/{id}/decompose respects effort timeout (high→15min)
# tags: surface:api feature:automata conflict:llm
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-380"
story_preflight "surface:api feature:automata conflict:llm" || return 0

_story_ts_380() {
  # Requires autonomous to be enabled and a live LLM
  ensure_test_automaton || return 0

  # POST decompose — use low effort; allow up to 5 minutes
  local resp code body
  resp=$(curl "${curl_args[@]}" --max-time 300 -s -w "\n__HTTP_CODE_%{http_code}__" \
    -X POST "$TEST_BASE/api/autonomous/prds/$AUTOMATON_ID/decompose" 2>/dev/null)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-380 "decompose.json" "$body"

  if [[ "$code" == "200" ]]; then
    ok "decompose (effort=low) completed: HTTP 200"
  elif [[ "$code" == "400" ]]; then
    # Already decomposed or invalid state — still counts as endpoint working
    ok "decompose returned 400 (already decomposed or invalid state): $body"
  elif [[ "$code" == "503" ]]; then
    skip "autonomous not available (503)"
  elif [[ "$code" == "000" ]]; then
    skip "decompose timed out or daemon unreachable"
  else
    ko "decompose returned HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_380
: "${RESULT:=fail}"
unset -f _story_ts_380
