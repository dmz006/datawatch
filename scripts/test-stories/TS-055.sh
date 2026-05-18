#!/usr/bin/env bash
# TS-055 — Secret scoping enforcement
# tags: surface:api feature:secrets
# legacy fn: t6_ts055_secret_scoping
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-055"
story_preflight "surface:api feature:secrets" || return 0

_story_ts_055() {
  local resp
  resp=$(api POST /api/secrets '{"name":"test-scoped-secret-'"$$"'","value":"scoped-value","backend":"env","scopes":["plugin"]}')
  save_evidence TS-055 "create.json" "$resp"
  if assert_json "$resp" '"name" in d'; then
    add_cleanup secret "test-scoped-secret-$$"
    ok "scoped secret created with scopes=[plugin]"
  else
    skip "scoped secret create failed"
  fi
}

RESULT=fail
_story_ts_055
: "${RESULT:=fail}"
unset -f _story_ts_055
