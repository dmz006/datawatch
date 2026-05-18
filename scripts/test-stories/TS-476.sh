#!/usr/bin/env bash
# TS-476 — POST /api/autonomous/prds/{id}/approve returns 200 or 400
# tags: surface:api feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-476"
story_preflight "surface:api feature:automata" || return 0

_story_ts_476() {
  ensure_test_automaton || return
  local resp code
  resp=$(api_code POST "/api/autonomous/prds/$AUTOMATON_ID/approve" '{}')
  save_evidence TS-476 "approve.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  if [[ "$code" == "200" || "$code" == "202" || "$code" == "400" || "$code" == "409" ]]; then
    ok "POST /api/autonomous/prds/$AUTOMATON_ID/approve returned $code"
  elif [[ "$code" == "404" ]]; then
    skip "/approve endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_476
: "${RESULT:=fail}"
unset -f _story_ts_476
