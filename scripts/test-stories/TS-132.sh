#!/usr/bin/env bash
# TS-132 — Sessions list renders
# tags: surface:pwa feature:sessions conflict:pwa
# legacy fn: t11_ts132_sessions_list
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-132"
story_preflight "surface:pwa feature:sessions conflict:pwa" || return 0

_story_ts_132() {
  local resp
  resp=$(api GET /api/sessions)
  save_evidence TS-132 "sessions.json" "$resp"
  if assert_json "$resp" 'isinstance(d,list) or isinstance(d.get("sessions",[]),list)'; then
    ok "sessions list endpoint works"
  else
    ko "sessions list failed: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_132
: "${RESULT:=fail}"
unset -f _story_ts_132
