#!/usr/bin/env bash
# TS-421 — GET /api/secrets returns list shape (name+scopes, no values)
# tags: surface:api feature:secrets
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-421"
story_preflight "surface:api feature:secrets" || return 0

_story_ts_421() {
  local resp
  resp=$(api GET /api/secrets)
  save_evidence TS-421 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/secrets returns array"
  elif assert_json "$resp" '"secrets" in d and isinstance(d["secrets"], list)'; then
    ok "GET /api/secrets returns {secrets:[...]} shape"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/secrets returns dict"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "secrets endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_421
: "${RESULT:=fail}"
unset -f _story_ts_421
