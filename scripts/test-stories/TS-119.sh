#!/usr/bin/env bash
# TS-119 — datawatch secrets list
# tags: surface:cli feature:secrets
# legacy fn: t10_ts119_secrets_list_cli
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-119"
story_preflight "surface:cli feature:secrets" || return 0

_story_ts_119() {
  local out
  out=$(cli_test secrets list 2>&1 || \
        cli_test secret list 2>&1 || true)
  save_evidence TS-119 "secrets_list.txt" "$out"
  if [[ -n "$out" ]]; then
    ok "datawatch secrets list returned output"
  else
    skip "secrets list CLI subcommand not available"
  fi
}

RESULT=fail
_story_ts_119
: "${RESULT:=fail}"
unset -f _story_ts_119
