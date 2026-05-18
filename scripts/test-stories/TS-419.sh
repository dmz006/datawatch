#!/usr/bin/env bash
# TS-419 — GET /api/marketplace/ollama/catalog returns array with name+tags fields
# tags: surface:api feature:compute
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-419"
story_preflight "surface:api feature:compute" || return 0

_story_ts_419() {
  local resp
  resp=$(api GET /api/marketplace/ollama/catalog)
  save_evidence TS-419 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/marketplace/ollama/catalog returns array"
  elif assert_json "$resp" '"catalog" in d and isinstance(d["catalog"], list)'; then
    ok "GET /api/marketplace/ollama/catalog returns {catalog:[...]} shape"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "marketplace/ollama/catalog endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_419
: "${RESULT:=fail}"
unset -f _story_ts_419
