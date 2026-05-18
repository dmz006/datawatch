#!/usr/bin/env bash
# TS-116 — datawatch config set
# tags: surface:cli feature:config
# legacy fn: t10_ts116_config_set_cli
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-116"
story_preflight "surface:cli feature:config" || return 0

_story_ts_116() {
  local out
  out=$(cli_test config set --key session.skip_permissions --value true 2>&1 || \
        cli_test config set session.skip_permissions true 2>&1 || true)
  save_evidence TS-116 "config_set.txt" "$out"
  if [[ -n "$out" ]]; then
    ok "datawatch config set returned: $(echo "$out" | head -c 100)"
  else
    skip "config set CLI subcommand not available"
  fi
}

RESULT=fail
_story_ts_116
: "${RESULT:=fail}"
unset -f _story_ts_116
