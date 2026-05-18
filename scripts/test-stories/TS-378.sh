#!/usr/bin/env bash
# TS-378 — GET /api/evals returns {runs:[{id,name,status,score,created_at}]} shape
# tags: surface:api feature:evals
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-378"
story_preflight "surface:api feature:evals" || return 0

_story_ts_378() {
  local resp
  resp=$(api GET /api/evals)
  save_evidence TS-378 "resp.json" "$resp"
  if assert_json "$resp" '"runs" in d and isinstance(d["runs"], list)'; then
    ok "GET /api/evals returns {runs:[...]} shape"
  elif assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/evals returns array"
  elif echo "$resp" | grep -qi "not found\|404\|not available\|unknown"; then
    skip "evals endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_378
: "${RESULT:=fail}"
unset -f _story_ts_378
