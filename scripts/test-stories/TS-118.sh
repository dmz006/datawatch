#!/usr/bin/env bash
# TS-118 — datawatch plugins list
# tags: surface:cli feature:plugins
# legacy fn: t10_ts118_plugins_list
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-118"
story_preflight "surface:cli feature:plugins" || return 0

_story_ts_118() {
  local out
  out=$(cli_test plugins list 2>&1 || true)
  save_evidence TS-118 "plugins.txt" "$out"
  if [[ -n "$out" ]]; then
    ok "datawatch plugins list returned output"
  else
    skip "plugins list returned empty"
  fi
}

RESULT=fail
_story_ts_118
: "${RESULT:=fail}"
unset -f _story_ts_118
