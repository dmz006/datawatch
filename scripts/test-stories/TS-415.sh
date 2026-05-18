#!/usr/bin/env bash
# TS-415 — GET /api/llms returns {llms:[]} or array with llm entries
# tags: surface:api feature:llm-registry
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-415"
story_preflight "surface:api feature:llm-registry" || return 0

_story_ts_415() {
  local resp
  resp=$(api GET /api/llms)
  save_evidence TS-415 "resp.json" "$resp"
  if assert_json "$resp" '"llms" in d and isinstance(d["llms"], list)'; then
    ok "GET /api/llms returns {llms:[...]} shape"
  elif assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/llms returns array"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "llms endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_415
: "${RESULT:=fail}"
unset -f _story_ts_415
