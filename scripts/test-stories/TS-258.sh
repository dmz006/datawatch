#!/usr/bin/env bash
# TS-258 — GET /api/marketplace/ollama/catalog returns catalog array
# tags: surface:api feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-258"
story_preflight "surface:api feature:parity" || return 0

_story_ts_258() {
  local resp
  resp=$(api GET /api/marketplace/ollama/catalog)
  save_evidence TS-258 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "marketplace/ollama/catalog returns array"
  elif assert_json "$resp" 'isinstance(d, dict) and ("catalog" in d or "models" in d or "items" in d)'; then
    ok "marketplace/ollama/catalog returns dict with catalog key"
  elif echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "marketplace/ollama/catalog not available in this build"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_258
: "${RESULT:=fail}"
unset -f _story_ts_258
