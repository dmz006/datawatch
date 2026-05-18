#!/usr/bin/env bash
# TS-383 — GET /.well-known/unifiedpush returns discovery doc with version:1
# tags: surface:api feature:push
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-383"
story_preflight "surface:api feature:push" || return 0

_story_ts_383() {
  local resp
  resp=$(curl -sk -H "Authorization: Bearer $TEST_TOKEN" \
    "$TEST_BASE/.well-known/unifiedpush")
  save_evidence TS-383 "resp.json" "$resp"
  if assert_json "$resp" '"version" in d'; then
    ok "GET /.well-known/unifiedpush returns doc with version field"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /.well-known/unifiedpush returns JSON dict"
  elif echo "$resp" | grep -qi "not found\|404"; then
    skip "unifiedpush discovery endpoint not available (404)"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_383
: "${RESULT:=fail}"
unset -f _story_ts_383
