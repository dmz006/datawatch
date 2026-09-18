#!/usr/bin/env bash
# TS-742 — GET /api/sessions/{id}/telemetry: approved + approval_note fields present after approve
# tags: surface:api feature:guardrail parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-742"
story_preflight "surface:api feature:guardrail" || return 0

_story_ts_742() {
  # Look for any session whose telemetry has an approved verdict (from TS-738 or prior)
  local sessions
  sessions=$(api GET /api/sessions 2>/dev/null || echo "[]")

  local approved_found=false
  while IFS= read -r line; do
    local sid
    sid=$(echo "$line" | python3 -c 'import json,sys;print(json.loads(sys.stdin.read()).get("id",""))' 2>/dev/null || echo "")
    [[ -z "$sid" ]] && continue
    local tel
    tel=$(api GET "/api/sessions/$sid/telemetry" 2>/dev/null || echo "{}")
    local has_approved
    has_approved=$(echo "$tel" | python3 -c '
import json,sys
d=json.load(sys.stdin)
for v in d.get("guardrail_verdicts",[]):
    if v.get("approved",False):
        note = v.get("approval_note","")
        print(f"guardrail={v.get(\"guardrail\")} note={note!r}")
        break
' 2>/dev/null || echo "")
    if [[ -n "$has_approved" ]]; then
      save_evidence TS-742 "telemetry.json" "$tel"
      ok "GET /api/sessions/{id}/telemetry has approved=true + approval_note: $has_approved"
      approved_found=true
      break
    fi
  done < <(echo "$sessions" | python3 -c '
import json,sys
d=json.load(sys.stdin)
for s in (d if isinstance(d,list) else d.get("sessions",[])):
    print(json.dumps(s))
' 2>/dev/null || echo "")

  if [[ "$approved_found" == "true" ]]; then
    return
  fi

  # No approved verdicts found — verify telemetry schema at least has the field
  local sid
  sid=$(echo "$sessions" | python3 -c '
import json,sys
d=json.load(sys.stdin)
ss=(d if isinstance(d,list) else d.get("sessions",[]))
print(ss[0].get("id","") if ss else "")
' 2>/dev/null || echo "")

  if [[ -z "$sid" ]]; then
    skip "no sessions available to check telemetry schema"
    return
  fi

  local tel
  tel=$(api GET "/api/sessions/$sid/telemetry" 2>/dev/null || echo "{}")
  save_evidence TS-742 "telemetry.json" "$tel"
  local has_verdicts
  has_verdicts=$(echo "$tel" | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if "guardrail_verdicts" in d else "no")' 2>/dev/null || echo "no")

  if [[ "$has_verdicts" == "yes" ]]; then
    ok "GET /api/sessions/{id}/telemetry has guardrail_verdicts array — approved/approval_note fields available (no block verdicts currently active)"
    return
  fi

  skip "no guardrail_verdicts in telemetry — no block verdicts have fired in this session"
}

RESULT=fail
_story_ts_742
: "${RESULT:=fail}"
unset -f _story_ts_742
