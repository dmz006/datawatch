#!/usr/bin/env bash
# TS-096 — !sessions comm command
# tags: surface:api feature:comms feature:sessions
# legacy fn: t9_ts096_sessions_command
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-096"
story_preflight "surface:api feature:comms feature:sessions" || return 0

_story_ts_096() {
  # Ensure at least one session exists so !sessions returns a non-empty list
  ensure_test_session || true
  local resp
  resp=$(api POST /api/test/message '{"text":"sessions"}')
  save_evidence TS-096 "sessions.json" "$resp"
  if assert_json "$resp" 'isinstance(d.get("responses",[]), list) and d.get("count",0) >= 1'; then
    ok "!sessions command returned responses"
  else
    ko "!sessions command failed: $resp"
  fi
}

RESULT=fail
_story_ts_096
: "${RESULT:=fail}"
unset -f _story_ts_096
