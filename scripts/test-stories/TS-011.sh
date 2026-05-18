#!/usr/bin/env bash
# TS-011 — List sessions
# tags: surface:api feature:sessions
# legacy fn: t2_ts011_list_sessions
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-011"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_011() {
  local resp
  resp=$(api GET /api/sessions)
  save_evidence TS-011 "sessions.json" "$resp"
  if assert_json "$resp" '"sessions" in d or isinstance(d, list)'; then
    ok "GET /api/sessions returns list shape"
  else
    ko "sessions list shape wrong: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_011
: "${RESULT:=fail}"
unset -f _story_ts_011
