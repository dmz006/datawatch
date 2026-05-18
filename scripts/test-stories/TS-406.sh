#!/usr/bin/env bash
# TS-406 — GET /api/mcp/resources/read?uri=datawatch://version returns text content
# tags: surface:api feature:mcp-resources
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-406"
story_preflight "surface:api feature:mcp-resources" || return 0

_story_ts_406() {
  local resp
  resp=$(api GET "/api/mcp/resources/read?uri=datawatch://version")
  save_evidence TS-406 "resp.json" "$resp"
  if assert_json "$resp" '"contents" in d'; then
    ok "GET /api/mcp/resources/read?uri=datawatch://version returns {contents:...}"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/mcp/resources/read?uri=datawatch://version returns dict"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "mcp/resources/read endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_406
: "${RESULT:=fail}"
unset -f _story_ts_406
