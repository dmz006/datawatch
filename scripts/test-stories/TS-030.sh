#!/usr/bin/env bash
# TS-030 — List personas
# tags: surface:api feature:council
# legacy fn: t4_ts030_list_personas
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-030"
story_preflight "surface:api feature:council" || return 0

_story_ts_030() {
  local resp
  resp=$(api GET /api/council/personas)
  save_evidence TS-030 "personas.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "GET /api/council/personas returns valid shape"
  else
    ko "personas list unexpected: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_030
: "${RESULT:=fail}"
unset -f _story_ts_030
