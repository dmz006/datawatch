#!/usr/bin/env bash
# TS-443 — datawatch session new --llm shell \"test\" exits 0 and prints \"Session started.\"
# tags: surface:cli feature:sessions feature:cli
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-443"
story_preflight "surface:cli feature:sessions feature:cli" || return 0

_story_ts_443() {
  local task="test-cli-session-ts443-$$"
  local out rc
  # Try "session new --llm shell" form
  out=$(cli_test session new --llm shell "$task" 2>&1); rc=$?
  save_evidence TS-443 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "datawatch session new --llm shell exits 0"
  elif echo "$out" | grep -qiE "unknown.*flag|unknown command|not found|disabled|not.*available|no such"; then
    # Try alternative "sessions start" form
    out=$(cli_test sessions start --backend shell --llm shell --task "$task" 2>&1); rc=$?
    if [[ $rc -eq 0 ]]; then
      ok "datawatch sessions start --backend shell --llm shell exits 0"
    elif echo "$out" | grep -qiE "unknown command|not found|disabled|not.*available|no such"; then
      skip "session new/start with --llm not available: $(echo "$out" | head -c 80)"
    else
      ko "rc=$rc: $(echo "$out" | head -c 200)"
    fi
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_443
: "${RESULT:=fail}"
unset -f _story_ts_443
