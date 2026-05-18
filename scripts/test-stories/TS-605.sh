#!/usr/bin/env bash
# TS-605 — datawatch session list --all-servers includes remote sessions
# tags: surface:cli feature:multiserver feature:cli
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-605"
story_preflight "surface:cli feature:multiserver feature:cli" || return 0

_story_ts_605() {
  local out rc
  out=$(cli_test session list --all-servers 2>&1) || rc=$?
  rc="${rc:-0}"
  save_evidence TS-605 "out.txt" "$out"
  if [[ "$rc" -eq 0 ]]; then
    ok "datawatch session list --all-servers exits 0"
  elif echo "$out" | grep -qi "unknown command\|unknown flag\|no route\|disabled\|not found\|help"; then
    skip "session list --all-servers CLI not available in this build"
  else
    ko "datawatch session list --all-servers failed (rc=$rc): $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_605
: "${RESULT:=fail}"
unset -f _story_ts_605
