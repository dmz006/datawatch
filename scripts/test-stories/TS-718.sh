#!/usr/bin/env bash
# TS-718 — Memory integration: GET /api/memory/stats returns valid JSON
# tags: surface:api feature:memory parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-718"
story_preflight "surface:api feature:memory" || return 0

_story_ts_718() {
  local resp code body
  resp=$(api_code GET /api/memory/stats)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-718 "memory_stats.json" "$body"

  if [[ "$code" == "503" ]]; then
    skip "memory service not available (503) — memory backend not configured"
    return
  fi
  if [[ "$code" == "404" ]]; then
    skip "GET /api/memory/stats returned 404 — endpoint may not be registered on this version"
    return
  fi
  if [[ ! "$code" =~ ^2 ]]; then
    ko "GET /api/memory/stats returned HTTP $code: $(echo "$body" | head -c 200)"
    return
  fi

  # Verify valid JSON
  if ! echo "$body" | python3 -c "import json,sys;json.load(sys.stdin)" 2>/dev/null; then
    ko "GET /api/memory/stats returned non-JSON: $(echo "$body" | head -c 200)"
    return
  fi

  ok "GET /api/memory/stats returned HTTP $code with valid JSON"
}

RESULT=fail
_story_ts_718
: "${RESULT:=fail}"
unset -f _story_ts_718
