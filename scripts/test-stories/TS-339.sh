#!/usr/bin/env bash
# TS-339 — datawatch tooling status exits 0
# tags: surface:cli feature:cli feature:plugins
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-339"
story_preflight "surface:cli feature:cli feature:plugins" || return 0

_story_ts_339() {
  local out; out=$(cli_test tooling status 2>&1); local rc=$?
  save_evidence TS-339 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "tooling status exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
    skip "tooling status not available: $(echo "$out" | head -c 80)"
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_339
: "${RESULT:=fail}"
unset -f _story_ts_339
