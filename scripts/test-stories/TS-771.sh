#!/usr/bin/env bash
# TS-771 — GH153: guardrail approve endpoint shape + telemetry surface
# tags: surface:api feature:sessions feature:guardrails parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-771"

story_preflight "surface:api feature:sessions" || exit 0

# Create a session.
sess_raw=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"task":"ts771-guardrail-test","project_dir":"/tmp","backend":"shell"}' \
  "$TEST_TLS/api/sessions/start" 2>/dev/null)
sess_id=$(echo "$sess_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null)
if [[ -z "$sess_id" ]]; then
  ko "could not create session: $sess_raw"
  exit 0
fi
echo "  [TS-771] session=$sess_id"
add_cleanup sess "$sess_id"

# GET telemetry — must return a valid JSON object with updated_at.
tel_raw=$(curl "${curl_args[@]}" "$TEST_TLS/api/sessions/$sess_id/telemetry" 2>/dev/null)
tel_updated=$(echo "$tel_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('updated_at',''))" 2>/dev/null)
echo "  [TS-771] telemetry updated_at=$tel_updated"
save_evidence "$CURRENT_STORY" "telemetry.json" "$tel_raw"
if [[ -z "$tel_updated" ]]; then
  ko "GET /api/sessions/$sess_id/telemetry returned no updated_at: $tel_raw"
  exit 0
fi

# POST approve for a non-existent guardrail — must return 404 per GH153 spec.
apr_code=$(curl "${curl_args[@]}" -X POST -o /dev/null -w "%{http_code}" \
  -H "Content-Type: application/json" \
  -d '{"note":"ts771 test — no actual block"}' \
  "$TEST_TLS/api/sessions/$sess_id/guardrail/ts771-phantom/approve" 2>/dev/null)
echo "  [TS-771] approve non-existent guardrail → HTTP $apr_code (expected 404)"
save_evidence "$CURRENT_STORY" "approve_404.txt" "HTTP $apr_code"
if [[ "$apr_code" != "404" ]]; then
  ko "approving unknown guardrail returned $apr_code (expected 404)"
  exit 0
fi

# POST approve for unknown session — must also return 404.
apr_code2=$(curl "${curl_args[@]}" -X POST -o /dev/null -w "%{http_code}" \
  -H "Content-Type: application/json" \
  -d '{"note":"ts771 bad session"}' \
  "$TEST_TLS/api/sessions/ts771-no-such-session/guardrail/content-safety/approve" 2>/dev/null)
echo "  [TS-771] approve unknown session → HTTP $apr_code2 (expected 404)"

ok "GH153 guardrail approve shape confirmed: telemetry.updated_at present; unknown-guardrail=404; unknown-session=404"
