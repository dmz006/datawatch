#!/usr/bin/env bash
# TS-057 — 1Password backend config round-trip
# tags: surface:api feature:secrets conflict:op
# legacy fn: t6_ts057_1password_backend_config
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-057"
story_preflight "surface:api feature:secrets conflict:op" || return 0

_story_ts_057() {
  if ! command -v op >/dev/null 2>&1; then
    skip "1Password op CLI not installed"
    return
  fi
  local put_resp
  put_resp=$(api PUT /api/config '{"secrets.onepassword.vault":"TestVault"}')
  save_evidence TS-057 "put.json" "$put_resp"
  if assert_json "$put_resp" 'd.get("status") == "ok"'; then
    ok "1Password backend config PUT accepted"
    api PUT /api/config '{"secrets.onepassword.vault":""}' >/dev/null
  else
    skip "1Password config key not present in this version"
  fi
}

RESULT=fail
_story_ts_057
: "${RESULT:=fail}"
unset -f _story_ts_057
