#!/usr/bin/env bash
# TS-097 — !status comm command
# tags: surface:api feature:comms
# legacy fn: t9_ts097_status_command
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-097"
story_preflight "surface:api feature:comms" || return 0

_story_ts_097() {
  ensure_test_session || return
  local resp
  resp=$(api POST /api/test/message '{"text":"status"}')
  save_evidence TS-097 "status.json" "$resp"
  if assert_json "$resp" 'd.get("count", 0) >= 1'; then
    ok "!status command returned response"
  else
    ko "!status command failed: $resp"
  fi
}

RESULT=fail
_story_ts_097
: "${RESULT:=fail}"
unset -f _story_ts_097
