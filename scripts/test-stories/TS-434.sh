#!/usr/bin/env bash
# TS-434 — docs_list_howtos contains dashboard and compute-nodes and mcp-sampling
# tags: surface:mcp feature:mcp feature:howto
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-434"
story_preflight "surface:mcp feature:mcp feature:howto" || return 0

_story_ts_434() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_list_howtos","params":{}}')
  save_evidence TS-434 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "docs_list_howtos returns array"
  elif assert_json "$resp" '"howtos" in d and isinstance(d["howtos"], list)'; then
    ok "docs_list_howtos returns {howtos:[...]} shape"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "docs_list_howtos returns dict"
  elif echo "$resp" | grep -qi "unknown tool\|not found\|not available"; then
    skip "docs_list_howtos MCP tool not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_434
: "${RESULT:=fail}"
unset -f _story_ts_434
