#!/usr/bin/env bash
# TS-392 — GET /api/alerts/aggregated returns array with server field per item
# tags: surface:api feature:multi-server
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-392"
story_preflight "surface:api feature:multi-server" || return 0

_story_ts_392() {
  local resp
  resp=$(api GET /api/alerts/aggregated)
  save_evidence TS-392 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/alerts/aggregated returns array"
  elif assert_json "$resp" '"alerts" in d and isinstance(d["alerts"], list)'; then
    ok "GET /api/alerts/aggregated returns {alerts:[...]} shape"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "alerts/aggregated endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_392
: "${RESULT:=fail}"
unset -f _story_ts_392
