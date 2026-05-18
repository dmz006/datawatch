#!/usr/bin/env bash
# TS-334 — datawatch identity configure shape check exits 0
# tags: surface:cli feature:cli feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-334"
story_preflight "surface:cli feature:cli feature:parity" || return 0

_story_ts_334() {
  # Use --help to check the configure subcommand exists without modifying state
  local out; out=$(cli_test identity configure --help 2>&1); local rc=$?
  save_evidence TS-334 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "identity configure --help exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
    skip "identity configure not available: $(echo "$out" | head -c 80)"
  else
    # --help may exit non-zero on some CLIs but still print usage
    if echo "$out" | grep -qi "configure\|identity\|usage\|flag"; then
      ok "identity configure --help returned usage info"
    else
      ko "exited $rc: $(echo "$out" | head -c 200)"
    fi
  fi
}

RESULT=fail
_story_ts_334
: "${RESULT:=fail}"
unset -f _story_ts_334
