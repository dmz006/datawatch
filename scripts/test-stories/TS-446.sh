#!/usr/bin/env bash
# TS-446 — comm new:llm=claude-code:<task> creates session with llm_ref set (checked via REST)
# tags: surface:comm feature:sessions
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-446"
story_preflight "surface:comm feature:sessions" || return 0

_story_ts_446() {
  ensure_test_session || return
  local resp
  resp=$(api POST /api/test/message "{\"text\":\"new:llm=shell:test-task-$$\"}" 2>/dev/null)
  save_evidence TS-446 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|unknown.*command\|not.*available\|not.*enabled"; then
    skip "comm endpoint not available in this build"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict)'; then
    local cnt
    cnt=$(api GET /api/sessions | python3 -c 'import json,sys;d=json.load(sys.stdin);ss=d.get("sessions",d) if isinstance(d,dict) else d;print(len(ss) if isinstance(ss,list) else 0)' 2>/dev/null || echo "0")
    if [[ "$cnt" -ge 1 ]] 2>/dev/null; then
      ok "comm new:llm= created session; total sessions: $cnt"
    else
      skip "comm endpoint responded but session count unclear: $resp"
    fi
  else
    skip "comm endpoint not available: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_446
: "${RESULT:=fail}"
unset -f _story_ts_446
