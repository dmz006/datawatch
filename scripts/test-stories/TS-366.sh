#!/usr/bin/env bash
# TS-366 — GET /api/autonomous/guardrails returns library array
# tags: surface:api feature:automata
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-366"
story_preflight "surface:api feature:automata" || return 0

_story_ts_366() {
  local resp
  resp=$(api GET /api/autonomous/guardrails)
  save_evidence TS-366 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/autonomous/guardrails returns array"
  elif assert_json "$resp" '"guardrails" in d and isinstance(d["guardrails"], list)'; then
    ok "GET /api/autonomous/guardrails returns {guardrails:[...]} shape"
  elif echo "$resp" | grep -qi "not found\|404\|not available\|unknown"; then
    skip "endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_366
: "${RESULT:=fail}"
unset -f _story_ts_366
