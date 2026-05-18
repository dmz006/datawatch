#!/usr/bin/env bash
# TS-022 — Automata list
# tags: surface:api feature:automata
# legacy fn: t3_ts022_automata_list
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-022"
story_preflight "surface:api feature:automata" || return 0

_story_ts_022() {
  local resp
  resp=$(api GET /api/autonomous/prds)
  save_evidence TS-022 "list.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "GET /api/autonomous/prds returns list shape"
  else
    ko "Automata list failed: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_022
: "${RESULT:=fail}"
unset -f _story_ts_022
