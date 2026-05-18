#!/usr/bin/env bash
# TS-021 — Automaton GET
# tags: surface:api feature:automata
# legacy fn: t3_ts021_automaton_get
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-021"
story_preflight "surface:api feature:automata" || return 0

_story_ts_021() {
  ensure_test_automaton || return
  local resp
  resp=$(api GET "/api/autonomous/prds/$AUTOMATON_ID")
  save_evidence TS-021 "get.json" "$resp"
  if assert_json "$resp" 'd.get("id") == "'"$AUTOMATON_ID"'"'; then
    ok "GET Automaton returns correct record"
  else
    ko "Automaton GET failed: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_021
: "${RESULT:=fail}"
unset -f _story_ts_021
