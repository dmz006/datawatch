#!/usr/bin/env bash
# TS-029 — Automaton children list
# tags: surface:api feature:automata
# legacy fn: t3_ts029_automaton_children
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-029"
story_preflight "surface:api feature:automata" || return 0

_story_ts_029() {
  ensure_test_automaton || return
  local resp
  resp=$(api GET "/api/autonomous/prds/$AUTOMATON_ID/children")
  save_evidence TS-029 "children.json" "$resp"
  if assert_json "$resp" '"children" in d and isinstance(d["children"], list)'; then
    ok "GET /children returns {children:[]} shape"
  else
    ko "Automaton children list shape wrong: $resp"
  fi
}

RESULT=fail
_story_ts_029
: "${RESULT:=fail}"
unset -f _story_ts_029
