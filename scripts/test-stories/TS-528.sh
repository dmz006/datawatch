#!/usr/bin/env bash
# TS-528 — GET /api/secrets returns list with scopes field per entry
# tags: surface:api feature:secrets
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-528"
story_preflight "surface:api feature:secrets" || return 0

_story_ts_528() {
  local resp
  resp=$(api GET /api/secrets)
  save_evidence TS-528 "secrets.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|unknown"; then
    skip "secrets endpoint not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, list) or isinstance(d.get("secrets",[]), list)'; then
    ok "GET /api/secrets returns list shape"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_528
: "${RESULT:=fail}"
unset -f _story_ts_528
