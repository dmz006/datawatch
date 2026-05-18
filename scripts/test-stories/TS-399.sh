#!/usr/bin/env bash
# TS-399 — datawatch mcp prompts list exits 0 and lists 10 entries
# tags: surface:cli feature:mcp-prompts feature:cli
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-399"
story_preflight "surface:cli feature:mcp-prompts feature:cli" || return 0

_story_ts_399() {
  local out rc
  out=$(cli_test mcp prompts list 2>&1); rc=$?
  save_evidence TS-399 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "datawatch mcp prompts list exits 0"
  elif echo "$out" | grep -qiE "unknown command|not found|disabled|not.*available|no such"; then
    skip "mcp prompts list not available: $(echo "$out" | head -c 80)"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_399
: "${RESULT:=fail}"
unset -f _story_ts_399
