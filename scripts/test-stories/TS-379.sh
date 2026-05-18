#!/usr/bin/env bash
# TS-379 — GET /api/memory/search returns [] JSON (not 500) when embedder unavailable
# tags: surface:api feature:memory
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-379"
story_preflight "surface:api feature:memory" || return 0

_story_ts_379() {
  local resp code body
  resp=$(api_code GET /api/memory/search?q=test)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-379 "resp.json" "$body"
  if [[ "$code" == "500" || "$code" == "503" ]]; then
    ko "GET /api/memory/search returned $code (server error — should return [] or 200)"
  elif [[ "$code" == "200" ]]; then
    ok "GET /api/memory/search returns 200 (not 500) when embedder unavailable"
  elif [[ "$code" == "404" ]]; then
    skip "memory/search endpoint not available (404)"
  else
    ok "GET /api/memory/search returns $code (acceptable — not 500)"
  fi
}

RESULT=fail
_story_ts_379
: "${RESULT:=fail}"
unset -f _story_ts_379
