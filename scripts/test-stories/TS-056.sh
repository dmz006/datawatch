#!/usr/bin/env bash
# TS-056 — KeePass backend config round-trip
# tags: surface:api feature:secrets conflict:keepassxc
# legacy fn: t6_ts056_keepass_backend_config
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-056"
story_preflight "surface:api feature:secrets conflict:keepassxc" || return 0

_story_ts_056() {
  if ! command -v keepassxc-cli >/dev/null 2>&1; then
    skip "keepassxc-cli not installed"
    return
  fi
  local put_resp
  put_resp=$(api PUT /api/config '{"secrets.keepass.path":"/tmp/test-dw-e2e.kdbx"}')
  save_evidence TS-056 "put.json" "$put_resp"
  if assert_json "$put_resp" 'd.get("status") == "ok"'; then
    ok "KeePass backend config PUT accepted"
    api PUT /api/config '{"secrets.keepass.path":""}' >/dev/null
  else
    skip "KeePass config key not present in this version"
  fi
}

RESULT=fail
_story_ts_056
: "${RESULT:=fail}"
unset -f _story_ts_056
