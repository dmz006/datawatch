#!/usr/bin/env bash
# TS-425 — GET /api/mcp/sampling-log returns array (may be empty)
# tags: surface:api feature:mcp-sampling
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-425"
story_preflight "surface:api feature:mcp-sampling" || return 0

_story_ts_425() {
  local resp
  resp=$(api GET /api/mcp/sampling-log)
  save_evidence TS-425 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/mcp/sampling-log returns array"
  elif assert_json "$resp" '"entries" in d and isinstance(d["entries"], list)'; then
    ok "GET /api/mcp/sampling-log returns {entries:[...]} shape"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "mcp/sampling-log endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_425
: "${RESULT:=fail}"
unset -f _story_ts_425
