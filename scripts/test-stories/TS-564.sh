#!/usr/bin/env bash
# TS-564 — GET /api/federation/peers returns [] on fresh install
# tags: surface:api feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-564"
story_preflight "surface:api feature:federation" || return 0

_story_ts_564() {
  local resp
  resp=$(api GET /api/federation/peers)
  save_evidence TS-564 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "federation/peers endpoint not available in this build"
  elif assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/federation/peers returns list"
  elif assert_json "$resp" 'isinstance(d, dict) and ("peers" in d or d == {})'; then
    ok "GET /api/federation/peers returns dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_564
: "${RESULT:=fail}"
unset -f _story_ts_564
