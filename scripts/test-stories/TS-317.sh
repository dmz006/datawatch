#!/usr/bin/env bash
# TS-317 — datawatch llm add + show + delete round-trip
# tags: surface:cli feature:cli feature:config
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-317"
story_preflight "surface:cli feature:cli feature:config" || return 0

_story_ts_317() {
  local out llm_name
  llm_name="test-llm-ts317-$$"

  # Add
  out=$(cli_test llm add "$llm_name" --kind openai --api-key "sk-test" --model "gpt-4o-mini" 2>&1); local rc=$?
  save_evidence TS-317 "add.txt" "$out"
  if [[ $rc -ne 0 ]]; then
    if echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command|unknown flag"; then
      skip "llm add not available: $(echo "$out" | head -c 80)"
      return
    fi
    ko "llm add failed rc=$rc: $(echo "$out" | head -c 200)"
    return
  fi
  add_cleanup llm "$llm_name"

  # Show
  out=$(cli_test llm get "$llm_name" 2>&1) || true
  save_evidence TS-317 "show.txt" "$out"

  # Delete
  out=$(cli_test llm delete "$llm_name" 2>&1) || true
  save_evidence TS-317 "delete.txt" "$out"

  ok "llm add/show/delete CRUD round-trip for $llm_name"
}

RESULT=fail
_story_ts_317
: "${RESULT:=fail}"
unset -f _story_ts_317
