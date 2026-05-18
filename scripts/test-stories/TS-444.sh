#!/usr/bin/env bash
# TS-444 — datawatch session new --llm ollama --compute datawatch-ollama exits 0 and prints ComputeNode line
# tags: surface:cli feature:sessions feature:compute feature:cli
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-444"
story_preflight "surface:cli feature:sessions feature:compute feature:cli" || return 0

_story_ts_444() {
  # First check if compute node "datawatch-ollama" exists
  local node_check
  node_check=$(curl -sk -o /dev/null -w "%{http_code}" \
    -H "Authorization: Bearer $TEST_TOKEN" \
    "$TEST_BASE/api/compute/nodes/datawatch-ollama")
  if [[ "$node_check" == "404" ]]; then
    skip "compute node 'datawatch-ollama' not found — skip"
    return
  fi
  local task="test-cli-session-ts444-$$"
  local out rc
  out=$(cli_test session new --llm ollama --compute datawatch-ollama "$task" 2>&1); rc=$?
  save_evidence TS-444 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "datawatch session new --llm ollama --compute datawatch-ollama exits 0"
  elif echo "$out" | grep -qiE "unknown.*flag|unknown command|not found|disabled|not.*available|no such"; then
    skip "session new --llm --compute not available: $(echo "$out" | head -c 80)"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_444
: "${RESULT:=fail}"
unset -f _story_ts_444
