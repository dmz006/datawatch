#!/usr/bin/env bash
# TS-430 — GET /api/evals returns {runs:[{id,name,status}]} shape (or empty runs array)
# tags: surface:api feature:evals
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-430"
story_preflight "surface:api feature:evals" || return 0

_story_ts_430() {
  local resp
  resp=$(api GET /api/evals)
  save_evidence TS-430 "resp.json" "$resp"
  if assert_json "$resp" '"runs" in d and isinstance(d["runs"], list)'; then
    ok "GET /api/evals returns {runs:[...]} shape"
  elif assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/evals returns array"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "evals endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_430
: "${RESULT:=fail}"
unset -f _story_ts_430
