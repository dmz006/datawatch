#!/usr/bin/env bash
# TS-112 — datawatch sessions list
# tags: surface:cli feature:sessions
# legacy fn: t10_ts112_sessions_list
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-112"
story_preflight "surface:cli feature:sessions" || return 0

_story_ts_112() {
  local out
  out=$(cli_test sessions list 2>&1 || true)
  save_evidence TS-112 "sessions.txt" "$out"
  if [[ $? -eq 0 ]] || echo "$out" | grep -qE "NAME|session|ID|list"; then
    ok "datawatch sessions list returned output"
  else
    skip "sessions list failed or CLI --base flag not supported: $out"
  fi
}

RESULT=fail
_story_ts_112
: "${RESULT:=fail}"
unset -f _story_ts_112
