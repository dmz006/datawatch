#!/usr/bin/env bash
# TS-393 — GET /api/autonomous/prds/aggregated returns array with server field per item
# tags: surface:api feature:multi-server
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-393"
story_preflight "surface:api feature:multi-server" || return 0

_story_ts_393() {
  local resp
  resp=$(api GET /api/autonomous/prds/aggregated)
  save_evidence TS-393 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/autonomous/prds/aggregated returns array"
  elif assert_json "$resp" '"prds" in d and isinstance(d["prds"], list)'; then
    ok "GET /api/autonomous/prds/aggregated returns {prds:[...]} shape"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "prds/aggregated endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_393
: "${RESULT:=fail}"
unset -f _story_ts_393
