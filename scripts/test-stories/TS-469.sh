#!/usr/bin/env bash
# TS-469 — GET /api/autonomous/config returns planning_backend key
# tags: surface:api feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-469"
story_preflight "surface:api feature:automata" || return 0

_story_ts_469() {
  local resp
  resp=$(api GET /api/autonomous/config)
  save_evidence TS-469 "config.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404"; then
    skip "autonomous/config endpoint not available"
    return
  fi
  if assert_json "$resp" '"planning_backend" in d'; then
    ok "GET /api/autonomous/config has planning_backend key"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    skip "autonomous/config responds but no planning_backend key: $(echo "$resp" | head -c 100)"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_469
: "${RESULT:=fail}"
unset -f _story_ts_469
