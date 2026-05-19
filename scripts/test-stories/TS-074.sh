#!/usr/bin/env bash
# TS-074 — Read datawatch://version resource
# tags: surface:mcp feature:mcp
# legacy fn: t8_ts074_version_resource
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-074"
story_preflight "surface:mcp feature:mcp" || return 0

_story_ts_074() {
  local resp
  resp=$(api GET "/api/mcp/resources/read?uri=datawatch://version")
  save_evidence TS-074 "version_resource.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict) and "contents" in d and isinstance(d["contents"], list)'; then
    ok "datawatch://version resource readable (contents array present)"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ko "response is dict but missing 'contents' array: $(echo "$resp" | head -c 200)"
  else
    ko "unexpected response from GET /api/mcp/resources/read?uri=datawatch://version: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_074
: "${RESULT:=fail}"
unset -f _story_ts_074
