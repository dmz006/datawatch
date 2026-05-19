#!/usr/bin/env bash
# TS-075 — Read datawatch://sessions resource
# tags: surface:mcp feature:mcp
# legacy fn: t8_ts075_sessions_resource
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-075"
story_preflight "surface:mcp feature:mcp" || return 0

_story_ts_075() {
  ensure_test_session || true  # best-effort: resource should still be readable even if empty
  local resp
  resp=$(api GET "/api/mcp/resources/read?uri=datawatch://sessions")
  save_evidence TS-075 "sessions_resource.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict) and "contents" in d and isinstance(d["contents"], list)'; then
    ok "datawatch://sessions resource readable (contents array present)"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ko "response is dict but missing 'contents' array: $(echo "$resp" | head -c 200)"
  else
    ko "unexpected response from GET /api/mcp/resources/read?uri=datawatch://sessions: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_075
: "${RESULT:=fail}"
unset -f _story_ts_075
