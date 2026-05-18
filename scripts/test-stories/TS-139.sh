#!/usr/bin/env bash
# TS-139 — Council personas list in PWA
# tags: surface:pwa feature:council conflict:pwa
# legacy fn: t11_ts139_council_personas
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-139"
story_preflight "surface:pwa feature:council conflict:pwa" || return 0

_story_ts_139() {
  local resp
  resp=$(api GET /api/council/personas)
  save_evidence TS-139 "personas.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list) or (isinstance(d, dict) and isinstance(d.get("personas",[]), list))'; then
    ok "council personas endpoint works"
  else
    ko "council personas endpoint failed: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_139
: "${RESULT:=fail}"
unset -f _story_ts_139
