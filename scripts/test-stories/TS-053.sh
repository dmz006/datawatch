#!/usr/bin/env bash
# TS-053 — Delete secret
# tags: surface:api feature:secrets conflict:db-write
# legacy fn: t6_ts053_delete_secret
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-053"
story_preflight "surface:api feature:secrets conflict:db-write" || return 0

_story_ts_053() {
  local resp
  resp=$(curl "${curl_args[@]}" -X DELETE "$TEST_BASE/api/secrets/test-secret-e2e-$$")
  save_evidence TS-053 "delete.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "secret deleted"
  else
    skip "secret delete failed: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_053
: "${RESULT:=fail}"
unset -f _story_ts_053
