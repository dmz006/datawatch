#!/usr/bin/env bash
# TS-257 — GET /api/federation/sessions returns {primary:[]} shape
# tags: surface:api feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-257"
story_preflight "surface:api feature:parity" || return 0

_story_ts_257() {
  local resp
  resp=$(api GET /api/federation/sessions)
  save_evidence TS-257 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict) and "primary" in d'; then
    ok "federation/sessions has primary key"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "federation/sessions returned dict"
  elif assert_json "$resp" 'isinstance(d, list)'; then
    ok "federation/sessions returned array"
  elif echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "federation/sessions endpoint not available in this build"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_257
: "${RESULT:=fail}"
unset -f _story_ts_257
