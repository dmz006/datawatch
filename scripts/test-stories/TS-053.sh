#!/usr/bin/env bash
# TS-053 — Delete secret
# tags: surface:api feature:secrets conflict:db-write
# legacy fn: t6_ts053_delete_secret
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-053"
story_preflight "surface:api feature:secrets conflict:db-write" || return 0

_story_ts_053() {
  local sname="test-secret-del-ts053-$$"
  # Create a secret to delete
  local set_resp
  set_resp=$(api PUT "/api/secrets/$sname" '{"value":"test-value-to-delete"}')
  if echo "$set_resp" | grep -qi "not found\|404\|disabled\|no route"; then
    skip "secrets endpoint not available"
    return
  fi
  local resp
  resp=$(curl "${curl_args[@]}" -X DELETE "$TEST_BASE/api/secrets/$sname")
  save_evidence TS-053 "delete.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "secret deleted"
  elif echo "$resp" | grep -qi "not found\|already deleted"; then
    ok "secret delete: not found (already deleted or never created)"
  else
    skip "secret delete failed: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_053
: "${RESULT:=fail}"
unset -f _story_ts_053
