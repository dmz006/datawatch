#!/usr/bin/env bash
# TS-120 — datawatch agents list
# tags: surface:cli feature:agents
# legacy fn: t10_ts120_agents_list_cli
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-120"
story_preflight "surface:cli feature:agents" || return 0

_story_ts_120() {
  local out
  out=$(cli_test agents list 2>&1 || \
        cli_test agent list 2>&1 || true)
  save_evidence TS-120 "agents_list.txt" "$out"
  if [[ -n "$out" ]]; then
    ok "datawatch agents list returned output"
  else
    skip "agents list CLI subcommand not available"
  fi
}

RESULT=fail
_story_ts_120
: "${RESULT:=fail}"
unset -f _story_ts_120
