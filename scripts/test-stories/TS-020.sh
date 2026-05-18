#!/usr/bin/env bash
# TS-020 — Create Automaton via REST
# tags: surface:api feature:automata blocking
# legacy fn: t3_ts020_create_automaton
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-020"
story_preflight "surface:api feature:automata blocking" || return 0

_story_ts_020() {
  if [[ "$(t3_check_autonomous)" != "yes" ]]; then skip "autonomous disabled"; return; fi
  local resp
  resp=$(api POST /api/autonomous/prds '{"spec":"test-prd-001: echo hello world","project_dir":"/tmp","backend":"claude-code","effort":"low"}')
  save_evidence TS-020 "create.json" "$resp"
  AUTOMATON_ID=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -n "$AUTOMATON_ID" ]]; then
    add_cleanup automaton "$AUTOMATON_ID"
    ok "Automaton created: $AUTOMATON_ID"
  else
    ko "Automaton create failed: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_020
: "${RESULT:=fail}"
unset -f _story_ts_020
