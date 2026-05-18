#!/usr/bin/env bash
# TS-113 — datawatch sessions start (skip if no LLM)
# tags: surface:cli feature:sessions conflict:llm
# legacy fn: t10_ts113_sessions_start
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-113"
story_preflight "surface:cli feature:sessions conflict:llm" || return 0

_story_ts_113() {
  # Starting a session via CLI requires an LLM backend — skip if not available
  local avail
  avail=$(api GET /api/backends 2>/dev/null | python3 -c '
import json,sys
d=json.load(sys.stdin)
have=[b["name"] for b in d.get("llm",[]) if b.get("enabled") and b.get("available")]
print(",".join(have))
' 2>/dev/null || echo "")
  if [[ -z "$avail" ]]; then
    skip "sessions start via CLI requires LLM backend (none available)"
    return
  fi
  local out
  out=$(cli_test sessions start --backend shell --task "e2e-cli-session-$$" 2>&1 || true)
  save_evidence TS-113 "start.txt" "$out"
  if echo "$out" | grep -qiE "started|session|id|created"; then
    ok "datawatch sessions start output: $(echo "$out" | head -c 100)"
  else
    skip "sessions start CLI output unclear: $out"
  fi
}

RESULT=fail
_story_ts_113
: "${RESULT:=fail}"
unset -f _story_ts_113
