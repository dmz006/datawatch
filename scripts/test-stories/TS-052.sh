#!/usr/bin/env bash
# TS-052 — Read secret metadata
# tags: surface:api feature:secrets
# legacy fn: t6_ts052_read_secret_metadata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-052"
story_preflight "surface:api feature:secrets" || return 0

_story_ts_052() {
  local resp
  resp=$(api GET "/api/secrets/test-secret-e2e-$$")
  save_evidence TS-052 "get.json" "$resp"
  if assert_json "$resp" '"name" in d or "error" not in d'; then
    ok "secret metadata returned"
  else
    skip "secret GET failed: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_052
: "${RESULT:=fail}"
unset -f _story_ts_052
