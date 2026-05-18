#!/usr/bin/env bash
# TS-321 — datawatch tailscale status exits 0
# tags: surface:cli feature:cli feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-321"
story_preflight "surface:cli feature:cli feature:parity" || return 0

_story_ts_321() {
  local out; out=$(cli_test tailscale status 2>&1); local rc=$?
  save_evidence TS-321 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "tailscale status exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command|not running"; then
    skip "tailscale not configured: $(echo "$out" | head -c 80)"
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_321
: "${RESULT:=fail}"
unset -f _story_ts_321
