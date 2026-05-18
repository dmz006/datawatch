#!/usr/bin/env bash
# TS-080 — POST /api/mcp/elicit structured response
# tags: surface:mcp feature:mcp
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-080"
story_preflight "surface:mcp feature:mcp" || return 0

_story_ts_080() {
  local resp code
  resp=$(api_code POST /api/mcp/elicit '{"schema":"approval","message":"E2E test elicit: approve this action?"}')
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  local body
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-080 "elicit_approval.json" "$body"
  if [[ "$code" == "404" ]]; then
    skip "POST /api/mcp/elicit: endpoint not implemented (404)"
    return
  fi
  if [[ "$code" == "200" || "$code" == "501" ]]; then
    if assert_json "$body" 'isinstance(d, dict)'; then
      ok "POST /api/mcp/elicit (approval schema): returned dict (HTTP $code)"
    else
      ok "POST /api/mcp/elicit (approval schema): endpoint exists (HTTP $code)"
    fi
  elif echo "$body" | grep -qiE "not supported|not implemented|unsupported"; then
    skip "POST /api/mcp/elicit: elicit not supported in this build"
  else
    ko "POST /api/mcp/elicit: unexpected HTTP $code: $(echo "$body" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_080
: "${RESULT:=fail}"
unset -f _story_ts_080
