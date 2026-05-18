#!/usr/bin/env bash
# TS-078 — POST /api/mcp/sample surface check
# tags: surface:mcp feature:mcp
# legacy fn: t8_ts078_mcp_sample_surface
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-078"
story_preflight "surface:mcp feature:mcp" || return 0

_story_ts_078() {
  local resp code
  resp=$(api_code POST /api/mcp/sample '{"messages":[{"role":"user","content":"ping"}],"maxTokens":10}')
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  save_evidence TS-078 "sample.json" "$(echo "$resp" | sed 's/__HTTP_CODE.*//')"
  if [[ "$code" == "200" || "$code" == "501" ]]; then
    ok "POST /api/mcp/sample: endpoint exists (HTTP $code)"
  elif [[ "$code" == "404" ]]; then
    skip "POST /api/mcp/sample: not implemented (v7.1.0 BL302 feature)"
  else
    ko "POST /api/mcp/sample: unexpected HTTP $code"
  fi
}

RESULT=fail
_story_ts_078
: "${RESULT:=fail}"
unset -f _story_ts_078
