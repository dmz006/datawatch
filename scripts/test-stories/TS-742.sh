#!/usr/bin/env bash
# TS-742 — GET /api/sessions/{id}/telemetry: guardrail_verdicts field present after scan guardrail fires
# tags: surface:api feature:guardrail parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-742"
story_preflight "surface:api feature:guardrail" || return 0

_story_ts_742() {
  # First, look for any existing session whose telemetry already has guardrail_verdicts
  # (e.g. from TS-738/TS-739 running earlier in the suite).
  local sessions
  sessions=$(api GET /api/sessions 2>/dev/null || echo "[]")
  while IFS= read -r line; do
    local sid
    sid=$(echo "$line" | python3 -c 'import json,sys;print(json.loads(sys.stdin.read()).get("id",""))' 2>/dev/null || echo "")
    [[ -z "$sid" ]] && continue
    local tel
    tel=$(api GET "/api/sessions/$sid/telemetry" 2>/dev/null || echo "{}")
    local has_gv
    has_gv=$(echo "$tel" | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if "guardrail_verdicts" in d else "no")' 2>/dev/null || echo "no")
    if [[ "$has_gv" == "yes" ]]; then
      save_evidence TS-742 "telemetry.json" "$tel"
      ok "GET /api/sessions/$sid/telemetry has guardrail_verdicts field (from prior run)"
      return
    fi
  done < <(echo "$sessions" | python3 -c '
import json,sys
d=json.load(sys.stdin)
for s in (d if isinstance(d,list) else d.get("sessions",[])):
    print(json.dumps(s))
' 2>/dev/null || echo "")

  # No existing verdict — create one inline via secrets-scan (no LLM needed).
  local proj
  proj=$(mktemp -d /tmp/ts742-XXXXXX)
  printf 'AWS_SECRET_ACCESS_KEY="AKIAIOSFODNN7EXAMPLE12345678901234"\n' > "$proj/.env"

  local sess_raw sid
  sess_raw=$(curl "${curl_args[@]}" -s -X POST \
    -H "Content-Type: application/json" \
    -d "{\"task\":\"ts742-guardrail\",\"project_dir\":\"$proj\",\"backend\":\"shell\"}" \
    "$TEST_TLS/api/sessions/start" 2>/dev/null || echo "")
  sid=$(echo "$sess_raw" | python3 -c "import json,sys;print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")

  _cleanup() {
    api POST /api/sessions/state "{\"id\":\"$sid\",\"state\":\"killed\"}" >/dev/null 2>&1 || true
    rm -rf "$proj"
  }

  if [[ -z "$sid" ]]; then
    rm -rf "$proj"
    ko "could not create session for guardrail schema test"
    return
  fi

  # Fire a guardrail to populate telemetry.
  local verdict
  verdict=$(api POST "/api/sessions/$sid/guardrail" '{"name":"secrets-scan"}')
  save_evidence TS-742 "verdict.json" "$verdict"

  local outcome
  outcome=$(echo "$verdict" | python3 -c "import json,sys;print(json.load(sys.stdin).get('outcome',''))" 2>/dev/null || echo "")
  if echo "$verdict" | grep -qi "not found\|unavailable\|503"; then
    _cleanup; skip "guardrail library not available: $verdict"
    return
  fi

  local tel
  tel=$(api GET "/api/sessions/$sid/telemetry" 2>/dev/null || echo "{}")
  save_evidence TS-742 "telemetry.json" "$tel"

  local has_gv
  has_gv=$(echo "$tel" | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if "guardrail_verdicts" in d else "no")' 2>/dev/null || echo "no")

  _cleanup

  if [[ "$has_gv" == "yes" ]]; then
    ok "GET /api/sessions/{id}/telemetry has guardrail_verdicts after secrets-scan (outcome=$outcome)"
    return
  fi

  ko "guardrail_verdicts key missing from telemetry after invoking secrets-scan (outcome=$outcome): $tel"
}

RESULT=fail
_story_ts_742
: "${RESULT:=fail}"
unset -f _story_ts_742
unset -f _cleanup 2>/dev/null || true
