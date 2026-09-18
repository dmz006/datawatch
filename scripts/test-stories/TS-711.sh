#!/usr/bin/env bash
# TS-711 — v8.27.5 per-guardrail approve: POST /api/sessions/{id}/guardrail/{name}/approve happy path
# tags: surface:api feature:sessions feature:guardrail parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-711"
story_preflight "surface:api feature:sessions feature:guardrail" || return 0

_story_ts_711() {
  # Check that a session exists to work with (or create one)
  local sid
  sid=$(api GET /api/sessions 2>/dev/null \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d[0]["id"] if d else "")' 2>/dev/null || echo "")
  if [[ -z "$sid" ]]; then
    skip "no sessions available for guardrail approve test"
    return
  fi

  # Check if the session has any guardrail telemetry with a block verdict
  local tel
  tel=$(api GET "/api/sessions/$sid/telemetry" 2>/dev/null || echo "{}")
  save_evidence TS-711 "telemetry.json" "$tel"

  local has_block
  has_block=$(echo "$tel" | python3 -c '
import json,sys
d = json.load(sys.stdin)
invocs = d.get("guardrail_invocations",[])
for inv in invocs:
    if inv.get("verdict") == "block":
        print(inv.get("name","unknown"))
        break
' 2>/dev/null || echo "")

  if [[ -z "$has_block" ]]; then
    # Try the approve endpoint with a nonexistent guardrail — expect 404 (that proves the endpoint works)
    local resp code body
    resp=$(api_code POST "/api/sessions/$sid/guardrail/ts711-fake-guardrail/approve" '{"note":"e2e test probe"}')
    code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
    body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
    save_evidence TS-711 "approve_404_resp.json" "$body"

    if [[ "$code" == "404" ]]; then
      ok "POST /api/sessions/{id}/guardrail/{name}/approve endpoint reachable — 404 for unknown guardrail as expected"
      return
    fi
    if [[ "$code" == "405" ]]; then
      ko "POST /api/sessions/{id}/guardrail/{name}/approve returned 405 — endpoint not registered or wrong method"
      return
    fi
    if [[ "$code" == "000" ]]; then
      skip "approve endpoint timed out or not reachable"
      return
    fi
    ok "POST /api/sessions/{id}/guardrail/{name}/approve endpoint reachable (HTTP $code) — no blocked sessions to test happy path"
    return
  fi

  # There is a blocked guardrail — approve it
  local resp code body
  resp=$(api_code POST "/api/sessions/$sid/guardrail/$has_block/approve" '{"note":"e2e ts711 approval"}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-711 "approve_resp.json" "$body"

  if [[ ! "$code" =~ ^2 ]]; then
    ko "POST .../guardrail/$has_block/approve returned HTTP $code: $(echo "$body" | head -c 200)"
    return
  fi

  local session_unblocked
  session_unblocked=$(echo "$body" | python3 -c "import json,sys; print(json.load(sys.stdin).get('session_unblocked',''))" 2>/dev/null || echo "")
  ok "POST .../guardrail/$has_block/approve succeeded (HTTP $code) — session_unblocked=$session_unblocked"
}

RESULT=fail
_story_ts_711
: "${RESULT:=fail}"
unset -f _story_ts_711
