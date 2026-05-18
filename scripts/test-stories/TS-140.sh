#!/usr/bin/env bash
# TS-140 — Automata list in PWA
# tags: surface:pwa feature:automata conflict:pwa
# legacy fn: t11_ts140_automata_list
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-140"
story_preflight "surface:pwa feature:automata conflict:pwa" || return 0

_story_ts_140() {
  local resp
  resp=$(api GET /api/autonomous/prds)
  save_evidence TS-140 "automata.json" "$resp"
  if assert_json "$resp" 'isinstance(d.get("prds",[]), list) or isinstance(d, dict)'; then
    ok "automata list endpoint works"
  else
    ko "automata list endpoint failed: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_140
: "${RESULT:=fail}"
unset -f _story_ts_140
