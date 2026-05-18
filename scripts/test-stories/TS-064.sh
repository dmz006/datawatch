#!/usr/bin/env bash
# TS-064 — Skills list
# tags: surface:api feature:skills
# legacy fn: t7_ts064_list_skills
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-064"
story_preflight "surface:api feature:skills" || return 0

_story_ts_064() {
  local resp
  resp=$(api GET /api/skills)
  save_evidence TS-064 "skills.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "GET /api/skills returns valid shape"
  else
    ko "skills list unexpected: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_064
: "${RESULT:=fail}"
unset -f _story_ts_064
