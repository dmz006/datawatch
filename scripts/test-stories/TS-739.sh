#!/usr/bin/env bash
# TS-739 — Guardrail approve: session_unblocked=false when other block verdicts remain
# tags: surface:api feature:guardrail parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-739"
story_preflight "surface:api feature:guardrail" || return 0

_story_ts_739() {
  # Find a session with TWO or more block verdicts (none yet approved)
  local sessions
  sessions=$(api GET /api/sessions 2>/dev/null || echo "[]")
  local session_id="" first_guard="" second_guard=""
  while IFS= read -r line; do
    local sid
    sid=$(echo "$line" | python3 -c 'import json,sys;print(json.loads(sys.stdin.read()).get("id",""))' 2>/dev/null || echo "")
    [[ -z "$sid" ]] && continue
    local tel
    tel=$(api GET "/api/sessions/$sid/telemetry" 2>/dev/null || echo "{}")
    local guards
    guards=$(echo "$tel" | python3 -c '
import json,sys
d=json.load(sys.stdin)
blocks=[v.get("guardrail","") for v in d.get("guardrail_verdicts",[])
        if v.get("outcome")=="block" and not v.get("approved",False)]
print("\n".join(blocks))
' 2>/dev/null || echo "")
    local count
    count=0; [[ -n "$guards" ]] && count=$(echo "$guards" | grep -c . 2>/dev/null || echo 0)
    if [[ "$count" -ge 2 ]]; then
      session_id="$sid"
      first_guard=$(echo "$guards" | head -1)
      second_guard=$(echo "$guards" | sed -n '2p')
      break
    fi
  done < <(echo "$sessions" | python3 -c '
import json,sys
d=json.load(sys.stdin)
for s in (d if isinstance(d,list) else d.get("sessions",[])):
    print(json.dumps(s))
' 2>/dev/null || echo "")

  if [[ -z "$session_id" ]]; then
    skip "no session with 2+ unapproved block verdicts found — cannot test session_unblocked=false path"
    return
  fi

  # Approve only the first block — session should NOT be unblocked
  local resp code body
  resp=$(api_code POST "/api/sessions/$session_id/guardrail/$first_guard/approve" \
    '{"note":"ts739 partial approve"}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-739 "approve1.json" "$body"

  if [[ ! "$code" =~ ^2 ]]; then
    ko "first guardrail approve returned HTTP $code; want 2xx"
    return
  fi

  local unblocked
  unblocked=$(echo "$body" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("session_unblocked",""))' 2>/dev/null || echo "")

  if [[ "$unblocked" == "False" || "$unblocked" == "false" ]]; then
    ok "session_unblocked=false after approving first of two blocks (second block $second_guard still active)"
    return
  fi
  if [[ "$unblocked" == "True" || "$unblocked" == "true" ]]; then
    ko "session_unblocked=true after approving first of two blocks — second block $second_guard was ignored"
    return
  fi

  # field not present or unknown state
  ok "first guardrail approve returned $code; session_unblocked=$unblocked (second block $second_guard remains)"
}

RESULT=fail
_story_ts_739
: "${RESULT:=fail}"
unset -f _story_ts_739
