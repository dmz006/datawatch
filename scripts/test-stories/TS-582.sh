#!/usr/bin/env bash
# TS-582 — datawatch federation peer add exits 0
# tags: surface:cli feature:federation feature:cli
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-582"
story_preflight "surface:cli feature:federation feature:cli" || return 0

_story_ts_582() {
  local out rc
  out=$(cli_test federation peer add --name e2e-cli-peer-ts582 --url http://127.0.0.1:19999 --token test 2>&1) || rc=$?
  rc="${rc:-0}"
  save_evidence TS-582 "out.txt" "$out"
  if echo "$out" | grep -qi "unknown command\|unknown flag\|disabled\|no route\|help"; then
    skip "federation peer add CLI not available in this build"
    return
  fi
  if [[ "$rc" -eq 0 ]]; then
    ok "datawatch federation peer add exits 0"
    # cleanup
    cli_test federation peer delete e2e-cli-peer-ts582 >/dev/null 2>&1 || true
  else
    ko "datawatch federation peer add failed (rc=$rc): $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_582
: "${RESULT:=fail}"
unset -f _story_ts_582
