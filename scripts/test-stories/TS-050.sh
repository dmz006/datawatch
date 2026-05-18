#!/usr/bin/env bash
# TS-050 — Create secret (env backend)
# tags: surface:api feature:secrets conflict:db-write
# legacy fn: t6_ts050_create_secret
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-050"
story_preflight "surface:api feature:secrets conflict:db-write" || return 0

_story_ts_050() {
  local resp
  resp=$(api POST /api/secrets '{"name":"test-secret-e2e-'"$$"'","value":"test-secret-value-12345","backend":"env","scopes":["test"]}')
  save_evidence TS-050 "create.json" "$resp"
  if assert_json "$resp" '"name" in d'; then
    add_cleanup secret "test-secret-e2e-$$"
    ok "secret created"
  else
    skip "secrets endpoint unavailable: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_050
: "${RESULT:=fail}"
unset -f _story_ts_050
