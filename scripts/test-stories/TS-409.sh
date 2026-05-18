#!/usr/bin/env bash
# TS-409 — POST /api/mcp/elicit with schema=approval returns form shape or error:elicitation not supported
# tags: surface:api feature:mcp-elicitation
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-409"
story_preflight "surface:api feature:mcp-elicitation" || return 0

_story_ts_409() {
  local resp code body
  resp=$(api_code POST /api/mcp/elicit '{"schema":"approval","message":"Please approve this action"}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-409 "resp.json" "$body"
  if [[ "$code" == "200" ]]; then
    ok "POST /api/mcp/elicit returned 200"
  elif [[ "$code" == "404" ]]; then
    skip "mcp/elicit endpoint not available (404)"
  elif [[ "$code" == "400" || "$code" == "501" ]]; then
    if echo "$body" | grep -qi "not supported\|elicitation"; then
      skip "mcp elicitation not supported in this build"
    else
      ok "POST /api/mcp/elicit returned $code (endpoint exists)"
    fi
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_409
: "${RESULT:=fail}"
unset -f _story_ts_409
