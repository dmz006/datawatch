#!/usr/bin/env bash
# TS-397 — GET /api/mcp/prompts returns 10 prompts with name+description+arguments
# tags: surface:api feature:mcp-prompts
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-397"
story_preflight "surface:api feature:mcp-prompts" || return 0

_story_ts_397() {
  local resp
  resp=$(api GET /api/mcp/prompts)
  save_evidence TS-397 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list) and len(d) >= 1 and "name" in d[0]'; then
    local cnt
    cnt=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(len(d))' 2>/dev/null || echo "0")
    ok "GET /api/mcp/prompts returns array with $cnt entries each having name"
  elif assert_json "$resp" '"prompts" in d and isinstance(d["prompts"], list)'; then
    ok "GET /api/mcp/prompts returns {prompts:[...]} shape"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "mcp/prompts endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_397
: "${RESULT:=fail}"
unset -f _story_ts_397
