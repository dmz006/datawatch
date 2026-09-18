#!/usr/bin/env bash
# TS-716 — mcp-search: GET /api/web_search/stats returns valid JSON
# tags: surface:api feature:mcp-search parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-716"
story_preflight "surface:api feature:mcp-search" || return 0

_story_ts_716() {
  local resp code body
  resp=$(api_code GET /api/web_search/stats)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-716 "stats_resp.json" "$body"

  if [[ "$code" == "503" || "$code" == "404" ]]; then
    skip "web_search/stats not available (HTTP $code) — web search feature may not be enabled or endpoint not registered"
    return
  fi
  if [[ ! "$code" =~ ^2 ]]; then
    ko "GET /api/web_search/stats returned HTTP $code: $(echo "$body" | head -c 200)"
    return
  fi

  # Verify response is valid JSON
  if ! echo "$body" | python3 -c "import json,sys;json.load(sys.stdin)" 2>/dev/null; then
    ko "GET /api/web_search/stats returned non-JSON: $(echo "$body" | head -c 200)"
    return
  fi

  ok "GET /api/web_search/stats returned HTTP $code with valid JSON"
}

RESULT=fail
_story_ts_716
: "${RESULT:=fail}"
unset -f _story_ts_716
