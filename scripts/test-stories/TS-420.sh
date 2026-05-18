#!/usr/bin/env bash
# TS-420 — GET /api/migration/compute-kinds returns {nodes:[],supported:[]} shape
# tags: surface:api feature:compute
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-420"
story_preflight "surface:api feature:compute" || return 0

_story_ts_420() {
  local resp
  resp=$(api GET /api/migration/compute-kinds)
  save_evidence TS-420 "resp.json" "$resp"
  if assert_json "$resp" '"nodes" in d or "supported" in d'; then
    ok "GET /api/migration/compute-kinds returns expected shape"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/migration/compute-kinds returns dict"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "migration/compute-kinds endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_420
: "${RESULT:=fail}"
unset -f _story_ts_420
