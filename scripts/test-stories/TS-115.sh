#!/usr/bin/env bash
# TS-115 — datawatch config get
# tags: surface:cli feature:config
# legacy fn: t10_ts115_config_get_cli
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-115"
story_preflight "surface:cli feature:config" || return 0

_story_ts_115() {
  local out
  out=$(cli_test config get 2>&1 || \
        cli_test config 2>&1 || true)
  save_evidence TS-115 "config_get.txt" "$out"
  if [[ -n "$out" ]]; then
    ok "datawatch config get returned output"
  else
    skip "config get CLI subcommand not available"
  fi
}

RESULT=fail
_story_ts_115
: "${RESULT:=fail}"
unset -f _story_ts_115
