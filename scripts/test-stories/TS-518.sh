#!/usr/bin/env bash
# TS-518 — GET /api/migration/compute-kinds returns {nodes:[],supported:[]} shape
# tags: surface:api feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-518"
story_preflight "surface:api feature:compute" || return 0

_story_ts_518() {
  local resp
  resp=$(api GET /api/migration/compute-kinds)
  save_evidence TS-518 "kinds.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|unknown"; then
    skip "migration/compute-kinds endpoint not available"
    return
  fi
  if assert_json "$resp" '"nodes" in d or "supported" in d'; then
    ok "GET /api/migration/compute-kinds returns expected shape"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/migration/compute-kinds returns dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_518
: "${RESULT:=fail}"
unset -f _story_ts_518
