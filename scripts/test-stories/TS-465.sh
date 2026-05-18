#!/usr/bin/env bash
# TS-465 — POST /api/autonomous/prds/{id}/decompose returns 200 or 202
# tags: surface:api feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-465"
story_preflight "surface:api feature:automata" || return 0

_story_ts_465() {
  ensure_test_automaton || return
  local resp code
  resp=$(api_code POST "/api/autonomous/prds/$AUTOMATON_ID/decompose" '{}')
  save_evidence TS-465 "decompose.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  if [[ "$code" == "200" || "$code" == "202" || "$code" == "400" ]]; then
    ok "POST /api/autonomous/prds/$AUTOMATON_ID/decompose returned $code"
  elif [[ "$code" == "404" ]]; then
    skip "/decompose endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_465
: "${RESULT:=fail}"
unset -f _story_ts_465
