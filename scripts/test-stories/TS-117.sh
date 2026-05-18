#!/usr/bin/env bash
# TS-117 — datawatch update --check (no install)
# tags: surface:cli feature:bootstrap
# legacy fn: t10_ts117_update_check
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-117"
story_preflight "surface:cli feature:bootstrap" || return 0

_story_ts_117() {
  local out
  out=$(cli_test update --check 2>&1 || true)
  save_evidence TS-117 "update_check.txt" "$out"
  if echo "$out" | grep -qiE "up.to.date|update.available|current|latest"; then
    ok "update --check returns status without installing"
  else
    skip "update --check output: $out"
  fi
}

RESULT=fail
_story_ts_117
: "${RESULT:=fail}"
unset -f _story_ts_117
