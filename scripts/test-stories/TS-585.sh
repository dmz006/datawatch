#!/usr/bin/env bash
# TS-585 — datawatch federation group add exits 0
# tags: surface:cli feature:federation feature:cli
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-585"
story_preflight "surface:cli feature:federation feature:cli" || return 0

_story_ts_585() {
  local out rc
  out=$(cli_test federation group add --name e2e-cli-grp-ts585 --caps sessions:list 2>&1) || rc=$?
  rc="${rc:-0}"
  save_evidence TS-585 "out.txt" "$out"
  if echo "$out" | grep -qi "unknown command\|unknown flag\|disabled\|no route\|help"; then
    skip "federation group add CLI not available in this build"
    return
  fi
  if [[ "$rc" -eq 0 ]]; then
    ok "datawatch federation group add exits 0"
    # cleanup
    cli_test federation group delete e2e-cli-grp-ts585 >/dev/null 2>&1 || true
  else
    ko "datawatch federation group add failed (rc=$rc): $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_585
: "${RESULT:=fail}"
unset -f _story_ts_585
