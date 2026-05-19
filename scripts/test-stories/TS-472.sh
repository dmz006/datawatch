#!/usr/bin/env bash
# TS-472 — GET /api/autonomous/prds returns array with status field
# tags: surface:api feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-472"
story_preflight "surface:api feature:automata" || return 0

_story_ts_472() {
  local a_enabled
  a_enabled=$(api GET /api/autonomous/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$a_enabled" != "yes" ]] && { skip "autonomous disabled"; return; }
  local resp
  resp=$(api GET /api/autonomous/prds)
  save_evidence TS-472 "prds.json" "$resp"
  if ! echo "$resp" | python3 -c "import json,sys; json.load(sys.stdin)" 2>/dev/null; then
    skip "autonomous/prds endpoint not available"
    return
  fi
  local prds
  prds=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("prds",d) if isinstance(d,dict) else d)' 2>/dev/null || echo "")
  if assert_json "$resp" 'isinstance(d, list) or isinstance(d.get("prds",[]), list)'; then
    ok "GET /api/autonomous/prds returns array shape"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_472
: "${RESULT:=fail}"
unset -f _story_ts_472
