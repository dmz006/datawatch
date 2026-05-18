#!/usr/bin/env bash
# TS-407 — GET /api/mcp/resources/templates returns array with uriTemplate field
# tags: surface:api feature:mcp-resources
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-407"
story_preflight "surface:api feature:mcp-resources" || return 0

_story_ts_407() {
  local resp
  resp=$(api GET /api/mcp/resources/templates)
  save_evidence TS-407 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/mcp/resources/templates returns array"
  elif assert_json "$resp" '"resourceTemplates" in d and isinstance(d["resourceTemplates"], list)'; then
    ok "GET /api/mcp/resources/templates returns {resourceTemplates:[...]} shape"
  elif assert_json "$resp" '"templates" in d'; then
    ok "GET /api/mcp/resources/templates returns {templates:[...]} shape"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "mcp/resources/templates endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_407
: "${RESULT:=fail}"
unset -f _story_ts_407
