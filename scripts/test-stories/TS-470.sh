#!/usr/bin/env bash
# TS-470 — YAML autonomous.planning_backend key is parsed and exposed via GET /api/config
# tags: surface:locale feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-470"
story_preflight "surface:locale feature:automata" || return 0

_story_ts_470() {
  local resp
  resp=$(api GET /api/config)
  save_evidence TS-470 "config.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict) and "autonomous" in d and "planning_backend" in d["autonomous"]'; then
    ok "GET /api/config exposes autonomous.planning_backend key (YAML key parsed and exposed)"
  elif assert_json "$resp" 'isinstance(d, dict) and "autonomous" in d'; then
    ko "autonomous section present but missing planning_backend key: $(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(list(d.get("autonomous",{}).keys()))' 2>/dev/null)"
  else
    ko "GET /api/config unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_470
: "${RESULT:=fail}"
unset -f _story_ts_470
