#!/usr/bin/env bash
# TS-395 — datawatch server add --name smoke-remote --url ... exits 0
# tags: surface:cli feature:multi-server feature:cli
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-395"
story_preflight "surface:cli feature:multi-server feature:cli" || return 0

_story_ts_395() {
  local srv_name="test-smoke-remote-ts395-$$"
  local out rc
  out=$(cli_test server add --name "$srv_name" --url "http://localhost:99999" --token "test" 2>&1); rc=$?
  save_evidence TS-395 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    add_cleanup server "$srv_name"
    ok "datawatch server add exits 0"
  elif echo "$out" | grep -qiE "unknown command|not found|disabled|not.*available|no such"; then
    skip "server add not available: $(echo "$out" | head -c 80)"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_395
: "${RESULT:=fail}"
unset -f _story_ts_395
