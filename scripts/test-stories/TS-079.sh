#!/usr/bin/env bash
# TS-079 — POST /api/mcp/elicit surface check
# tags: surface:mcp feature:mcp
# legacy fn: t8_ts079_mcp_elicit_surface
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-079"
story_preflight "surface:mcp feature:mcp" || return 0

_story_ts_079() {
  local resp code
  resp=$(api_code POST /api/mcp/elicit '{"requestedSchema":{"type":"object","properties":{"answer":{"type":"string"}}}}')
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  save_evidence TS-079 "elicit.json" "$(echo "$resp" | sed 's/__HTTP_CODE.*//')"
  if [[ "$code" == "200" || "$code" == "501" ]]; then
    ok "POST /api/mcp/elicit: endpoint exists (HTTP $code)"
  elif [[ "$code" == "404" ]]; then
    skip "POST /api/mcp/elicit: not implemented (v7.1.0 BL302 feature)"
  else
    ko "POST /api/mcp/elicit: unexpected HTTP $code"
  fi
}

RESULT=fail
_story_ts_079
: "${RESULT:=fail}"
unset -f _story_ts_079
