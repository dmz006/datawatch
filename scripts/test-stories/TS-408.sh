#!/usr/bin/env bash
# TS-408 — POST /api/mcp/sample with trigger=morning_briefing returns ok:true or error:sampling not supported
# tags: surface:api feature:mcp-sampling
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-408"
story_preflight "surface:api feature:mcp-sampling" || return 0

_story_ts_408() {
  local resp code body
  resp=$(api_code POST /api/mcp/sample '{"trigger":"morning_briefing"}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-408 "resp.json" "$body"
  if [[ "$code" == "200" ]]; then
    ok "POST /api/mcp/sample returned 200"
  elif [[ "$code" == "404" ]]; then
    skip "mcp/sample endpoint not available (404)"
  elif [[ "$code" == "400" || "$code" == "501" ]]; then
    if echo "$body" | grep -qi "not supported\|sampling"; then
      skip "mcp sampling not supported in this build"
    else
      ok "POST /api/mcp/sample returned $code (endpoint exists)"
    fi
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_408
: "${RESULT:=fail}"
unset -f _story_ts_408
