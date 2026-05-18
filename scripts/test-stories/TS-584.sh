#!/usr/bin/env bash
# TS-584 — datawatch federation group list exits 0
# tags: surface:cli feature:federation feature:cli
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-584"
story_preflight "surface:cli feature:federation feature:cli" || return 0

_story_ts_584() {
  local out rc
  out=$(cli_test federation group list 2>&1) || rc=$?
  rc="${rc:-0}"
  save_evidence TS-584 "out.txt" "$out"
  if [[ "$rc" -eq 0 ]]; then
    ok "datawatch federation group list exits 0"
  elif echo "$out" | grep -qi "unknown command\|no route\|disabled\|not found\|help"; then
    skip "federation group list CLI not available in this build"
  else
    ko "datawatch federation group list failed (rc=$rc): $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_584
: "${RESULT:=fail}"
unset -f _story_ts_584
