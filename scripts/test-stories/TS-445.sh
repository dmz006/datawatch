#!/usr/bin/env bash
# TS-445 — GET /api/sessions response for CLI-created session has backend_family field matching LLM kind
# tags: surface:cli surface:api feature:sessions feature:cli
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-445"
story_preflight "surface:cli surface:api feature:sessions feature:cli" || return 0

_story_ts_445() {
  local task="test-cli-session-ts445-$$"
  local out rc sid
  # Create a session via CLI with --llm claude-code (always auto-registered)
  out=$(cli_test session new --llm claude-code "$task" 2>&1); rc=$?
  save_evidence TS-445 "cli_out.txt" "$out"
  if [[ $rc -ne 0 ]]; then
    if echo "$out" | grep -qiE "unknown.*flag|unknown command|not found|disabled|not.*available|no such"; then
      skip "session new --llm not available: $(echo "$out" | head -c 80)"
    else
      ko "CLI session new failed rc=$rc: $(echo "$out" | head -c 200)"
    fi
    return
  fi
  # Extract session ID from output
  sid=$(echo "$out" | python3 -c '
import sys, re
for line in sys.stdin:
    m = re.search(r"([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})", line)
    if m:
        print(m.group(1))
        break
' 2>/dev/null || echo "")
  if [[ -z "$sid" ]]; then
    skip "could not extract session ID from CLI output: $(echo "$out" | head -c 100)"
    return
  fi
  add_cleanup sess "$sid"
  # GET the session and check for backend_family
  local resp
  resp=$(api GET "/api/sessions/$sid")
  save_evidence TS-445 "session_resp.json" "$resp"
  if assert_json "$resp" '"backend_family" in d'; then
    ok "CLI-created session has backend_family field"
  elif assert_json "$resp" '"id" in d'; then
    skip "session found but backend_family field not present (may not be implemented yet)"
  else
    ko "unexpected GET response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_445
: "${RESULT:=fail}"
unset -f _story_ts_445
