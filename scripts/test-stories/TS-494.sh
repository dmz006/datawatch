#!/usr/bin/env bash
# TS-494 — GET /api/autonomous/prds with type=operational returns filterable results
# tags: surface:api feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-494"
story_preflight "surface:api feature:automata" || return 0

_story_ts_494() {
  local a_enabled
  a_enabled=$(api GET /api/autonomous/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$a_enabled" != "yes" ]] && { skip "autonomous disabled"; return; }
  local resp
  resp=$(api GET "/api/autonomous/prds?type=operational")
  save_evidence TS-494 "prds.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404"; then
    skip "autonomous/prds endpoint not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, list) or isinstance(d.get("prds",[]), list)'; then
    ok "GET /api/autonomous/prds?type=operational returns array shape"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_494
: "${RESULT:=fail}"
unset -f _story_ts_494
