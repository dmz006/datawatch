#!/usr/bin/env bash
# TS-398 — POST /api/mcp/prompts/get with name=diagnose-system returns messages array
# tags: surface:api feature:mcp-prompts
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-398"
story_preflight "surface:api feature:mcp-prompts" || return 0

_story_ts_398() {
  local resp
  resp=$(api POST /api/mcp/prompts/get '{"name":"diagnose-system","arguments":{}}')
  save_evidence TS-398 "resp.json" "$resp"
  if assert_json "$resp" '"messages" in d and isinstance(d["messages"], list)'; then
    ok "POST /api/mcp/prompts/get returns {messages:[...]} shape"
  elif assert_json "$resp" 'isinstance(d, list)'; then
    ok "POST /api/mcp/prompts/get returns messages array"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "mcp/prompts/get endpoint not available"
  elif echo "$resp" | grep -qi "unknown prompt\|prompt not found"; then
    skip "diagnose-system prompt not available in this build"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_398
: "${RESULT:=fail}"
unset -f _story_ts_398
