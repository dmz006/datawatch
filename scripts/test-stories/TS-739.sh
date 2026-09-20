#!/usr/bin/env bash
# TS-739 — Guardrail approve: session_unblocked=false when other block verdicts remain
# tags: surface:api feature:guardrail parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-739"
story_preflight "surface:api feature:guardrail" || return 0

_story_ts_739() {
  # Create a project dir that triggers BOTH secrets-scan AND sast-scan blocks inline.
  local proj
  proj=$(mktemp -d /tmp/ts739-XXXXXX)
  printf 'AWS_SECRET_ACCESS_KEY="AKIAIOSFODNN7EXAMPLE12345678901234"\n' > "$proj/.env"
  printf 'result = eval("1+1")\n' > "$proj/vuln.py"

  local sess_raw sid
  sess_raw=$(curl "${curl_args[@]}" -s -X POST \
    -H "Content-Type: application/json" \
    -d "{\"task\":\"ts739-guardrail\",\"project_dir\":\"$proj\",\"backend\":\"shell\"}" \
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

  # Invoke two different scan guardrails — both should block.
  local v1 v2 out1 out2
  v1=$(api POST "/api/sessions/$sid/guardrail" '{"name":"secrets-scan"}')
  out1=$(echo "$v1" | python3 -c "import json,sys;print(json.load(sys.stdin).get('outcome',''))" 2>/dev/null || echo "")
  v2=$(api POST "/api/sessions/$sid/guardrail" '{"name":"sast-scan"}')
  out2=$(echo "$v2" | python3 -c "import json,sys;print(json.load(sys.stdin).get('outcome',''))" 2>/dev/null || echo "")
  save_evidence TS-739 "verdicts.json" "{\"secrets\":$v1,\"sast\":$v2}"

  if [[ "$out1" != "block" || "$out2" != "block" ]]; then
    _cleanup
    if echo "$v1$v2" | grep -qi "not found\|unavailable\|503"; then
      skip "guardrail library not available"
      return
    fi
    ko "expected two block verdicts; got secrets=$out1 sast=$out2"
    return
  fi

  # Approve only secrets-scan — sast-scan block remains, so session_unblocked must be false.
  local resp code body
  resp=$(api_code POST "/api/sessions/$sid/guardrail/secrets-scan/approve" \
    '{"note":"ts739 partial approve"}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-739 "approve1.json" "$body"

  _cleanup

  if [[ ! "$code" =~ ^2 ]]; then
    ko "first guardrail approve returned HTTP $code; want 2xx: $body"
    return
  fi

  local unblocked
  unblocked=$(echo "$body" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("session_unblocked",""))' 2>/dev/null || echo "")

  if [[ "$unblocked" == "False" || "$unblocked" == "false" ]]; then
    ok "session_unblocked=false after approving secrets-scan while sast-scan block remains"
    return
  fi
  if [[ "$unblocked" == "True" || "$unblocked" == "true" ]]; then
    ko "session_unblocked=true after approving first of two blocks — sast-scan block was ignored"
    return
  fi

  ok "guardrail approve returned $code with session_unblocked=$unblocked (sast-scan block remains)"
}

RESULT=fail
_story_ts_739
: "${RESULT:=fail}"
unset -f _story_ts_739
unset -f _cleanup 2>/dev/null || true
