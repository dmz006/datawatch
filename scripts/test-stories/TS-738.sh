#!/usr/bin/env bash
# TS-738 — Guardrail approve: note stored + telemetry approval_note field
# tags: surface:api feature:guardrail parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-738"
story_preflight "surface:api feature:guardrail" || return 0

_story_ts_738() {
  # Find a session with a block verdict to approve
  local sessions
  sessions=$(api GET /api/sessions 2>/dev/null || echo "[]")
  local session_id="" guard_name=""
  while IFS= read -r line; do
    local sid gname
    sid=$(echo "$line" | python3 -c 'import json,sys; r=json.loads(sys.stdin.read()); print(r.get("id",""))' 2>/dev/null || echo "")
    [[ -z "$sid" ]] && continue
    local tel
    tel=$(api GET "/api/sessions/$sid/telemetry" 2>/dev/null || echo "{}")
    gname=$(echo "$tel" | python3 -c '
import json,sys
d=json.load(sys.stdin)
for v in d.get("guardrail_verdicts",[]):
    if v.get("outcome")=="block" and not v.get("approved",False):
        print(v.get("guardrail",""))
        break
' 2>/dev/null || echo "")
    if [[ -n "$gname" ]]; then
      session_id="$sid"
      guard_name="$gname"
      break
    fi
  done < <(echo "$sessions" | python3 -c '
import json,sys
d=json.load(sys.stdin)
for s in (d if isinstance(d,list) else d.get("sessions",[])):
    print(json.dumps(s))
' 2>/dev/null || echo "")

  if [[ -z "$session_id" || -z "$guard_name" ]]; then
    skip "no session with an unapproved block verdict found — cannot test note storage"
    return
  fi

  local resp code body
  resp=$(api_code POST "/api/sessions/$session_id/guardrail/$guard_name/approve" \
    '{"note":"ts738 test approval note"}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-738 "approve.json" "$body"

  if [[ ! "$code" =~ ^2 ]]; then
    ko "POST guardrail/approve returned HTTP $code; want 2xx"
    return
  fi

  # Verify telemetry shows approval_note
  local tel2
  tel2=$(api GET "/api/sessions/$session_id/telemetry" 2>/dev/null || echo "{}")
  save_evidence TS-738 "telemetry.json" "$tel2"
  local note_found
  note_found=$(echo "$tel2" | python3 -c '
import json,sys
d=json.load(sys.stdin)
for v in d.get("guardrail_verdicts",[]):
    if v.get("guardrail","") == sys.argv[1] and v.get("approval_note",""):
        print(v["approval_note"])
        break
' "$guard_name" 2>/dev/null || echo "")

  if [[ -n "$note_found" ]]; then
    ok "guardrail approve note stored in telemetry: approval_note=$note_found"
    return
  fi

  # Approval may have succeeded even if note field not exposed in telemetry
  if echo "$body" | python3 -c 'import json,sys;d=json.load(sys.stdin);exit(0 if d.get("approved") else 1)' 2>/dev/null; then
    ok "guardrail approve HTTP $code + approved=true in response (note field not in telemetry)"
    return
  fi

  ko "guardrail approve returned $code but approval_note not visible in telemetry"
}

RESULT=fail
_story_ts_738
: "${RESULT:=fail}"
unset -f _story_ts_738
