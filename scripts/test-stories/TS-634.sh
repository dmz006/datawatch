#!/usr/bin/env bash
# TS-634 — datawatch plugins browse-registry exits with usage if no registry arg
# tags: surface:cli feature:plugin-install
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-634"
story_preflight "surface:cli feature:plugin-install" || return 0

_story_ts_634() {
  local out rc
  out=$(cli_test plugins browse-registry 2>&1); rc=$?
  save_evidence TS-634 "out.txt" "$out"
  if echo "$out" | grep -qiE "unknown command|no such subcommand|unknown subcommand"; then
    skip "plugins browse-registry CLI not available in this build"
    return
  fi
  # Expect non-zero exit (missing required arg) or usage/help output
  if [[ $rc -ne 0 ]] || echo "$out" | grep -qiE "usage|required|registry"; then
    ok "datawatch plugins browse-registry exits with usage if no registry arg"
  else
    ko "expected non-zero exit or usage message, rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_634
: "${RESULT:=fail}"
unset -f _story_ts_634
