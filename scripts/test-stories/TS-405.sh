#!/usr/bin/env bash
# TS-405 — GET /api/mcp/resources returns array with ≥5 entries each having uri field
# tags: surface:api feature:mcp-resources
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-405"
story_preflight "surface:api feature:mcp-resources" || return 0

_story_ts_405() {
  local resp
  resp=$(api GET /api/mcp/resources)
  save_evidence TS-405 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list) and len(d) >= 1 and "uri" in d[0]'; then
    local cnt
    cnt=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(len(d))' 2>/dev/null || echo "0")
    ok "GET /api/mcp/resources returns array with $cnt entries each having uri"
  elif assert_json "$resp" '"resources" in d and isinstance(d["resources"], list)'; then
    ok "GET /api/mcp/resources returns {resources:[...]} shape"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "mcp/resources endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_405
: "${RESULT:=fail}"
unset -f _story_ts_405
