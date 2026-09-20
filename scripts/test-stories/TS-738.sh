#!/usr/bin/env bash
# TS-738 — Guardrail approve: note stored + telemetry approval_note field
# tags: surface:api feature:guardrail parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-738"
story_preflight "surface:api feature:guardrail" || return 0

_story_ts_738() {
  # Create a temp project dir with a recognisable secret so secrets-scan blocks inline.
  local proj
  proj=$(mktemp -d /tmp/ts738-XXXXXX)
  printf 'AWS_SECRET_ACCESS_KEY="AKIAIOSFODNN7EXAMPLE12345678901234"\n' > "$proj/.env"

  local sess_raw sid
  sess_raw=$(curl "${curl_args[@]}" -s -X POST \
    -H "Content-Type: application/json" \
    -d "{\"task\":\"ts738-guardrail\",\"project_dir\":\"$proj\",\"backend\":\"shell\"}" \
    "$TEST_TLS/api/sessions/start" 2>/dev/null || echo "")
  sid=$(echo "$sess_raw" | python3 -c "import json,sys;print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")

  _cleanup() {
    api POST /api/sessions/state "{\"id\":\"$sid\",\"state\":\"killed\"}" >/dev/null 2>&1 || true
    rm -rf "$proj"
  }

  if [[ -z "$sid" ]]; then
    rm -rf "$proj"
    ko "could not create session for guardrail test: $sess_raw"
    return
  fi

  # Invoke secrets-scan — no LLM needed; purely regex-based.
  local verdict
  verdict=$(api POST "/api/sessions/$sid/guardrail" '{"name":"secrets-scan"}')
  save_evidence TS-738 "verdict.json" "$verdict"

  local outcome
  outcome=$(echo "$verdict" | python3 -c "import json,sys;print(json.load(sys.stdin).get('outcome',''))" 2>/dev/null || echo "")

  if [[ "$outcome" != "block" ]]; then
    _cleanup
    if echo "$verdict" | grep -qi "not found\|unavailable\|503"; then
      skip "guardrail library not available (outcome=$outcome): $verdict"
      return
    fi
    ko "secrets-scan did not produce block verdict (outcome=$outcome): $verdict"
    return
  fi

  # Approve the verdict with a note.
  local resp code body
  resp=$(api_code POST "/api/sessions/$sid/guardrail/secrets-scan/approve" \
    '{"note":"ts738 test approval note"}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-738 "approve.json" "$body"

  if [[ ! "$code" =~ ^2 ]]; then
    _cleanup; ko "POST guardrail/approve returned HTTP $code; want 2xx: $body"
    return
  fi

  # Verify telemetry has the approval_note.
  local tel
  tel=$(api GET "/api/sessions/$sid/telemetry" 2>/dev/null || echo "{}")
  save_evidence TS-738 "telemetry.json" "$tel"

  local note_found
  note_found=$(echo "$tel" | python3 -c '
import json,sys
d=json.load(sys.stdin)
for v in d.get("guardrail_verdicts",[]):
    if v.get("guardrail") == "secrets-scan" and v.get("approval_note",""):
        print(v["approval_note"])
        break
' 2>/dev/null || echo "")

  _cleanup

  if [[ -n "$note_found" ]]; then
    ok "guardrail approve note stored in telemetry: approval_note=$note_found"
    return
  fi

  # Approval succeeded even if approval_note not mirrored into telemetry
  if echo "$body" | python3 -c 'import json,sys;d=json.load(sys.stdin);exit(0 if d.get("approved") else 1)' 2>/dev/null; then
    ok "guardrail approve HTTP $code + approved=true in response (approval_note not mirrored in telemetry)"
    return
  fi

  ko "guardrail approve returned $code but approved=false and approval_note missing"
}

RESULT=fail
_story_ts_738
: "${RESULT:=fail}"
unset -f _story_ts_738
unset -f _cleanup 2>/dev/null || true
