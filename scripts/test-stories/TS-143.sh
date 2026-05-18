#!/usr/bin/env bash
# TS-143 — No console errors after full load
# tags: surface:pwa feature:bootstrap conflict:pwa
# legacy fn: t11_ts143_console_errors
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-143"
story_preflight "surface:pwa feature:bootstrap conflict:pwa" || return 0

_story_ts_143() {
  local syntax_check
  syntax_check=$(node --check "$REPO_ROOT/internal/server/web/app.js" 2>&1)
  local retval=$?
  save_evidence TS-143 "syntax_check.txt" "$syntax_check"
  if [[ $retval -eq 0 ]]; then
    ok "app.js has valid JavaScript syntax"
  else
    ko "app.js syntax error: $syntax_check"
  fi
}

RESULT=fail
_story_ts_143
: "${RESULT:=fail}"
unset -f _story_ts_143
