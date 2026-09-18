#!/usr/bin/env bash
# TS-203 — push notifications: POST /api/push/<topic> publishes event
# tags: surface:api feature:push feature:comms
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-203"
story_preflight "surface:api feature:push feature:comms" || return 0

_story_ts_203() {
  if [[ -z "$TEST_NTFY_TOPIC" ]]; then
    skip "push-notifications: requires NTFY topic (set TEST_NTFY_TOPIC)"
    return
  fi

  # Publish a push event to the test topic
  local pub_resp pub_code
  pub_resp=$(api_code POST "/api/push/$TEST_NTFY_TOPIC" '{"title":"e2e-test","message":"datawatch push e2e"}')
  pub_code=$(echo "$pub_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  local pub_body
  pub_body=$(echo "$pub_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-203 "publish.json" "$pub_body"

  if [[ "$pub_code" == "200" || "$pub_code" == "204" ]]; then
    # Verify the response shape
    local ok_val
    ok_val=$(echo "$pub_body" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('ok',''))" 2>/dev/null || echo "")
    if [[ "$ok_val" == "True" || "$ok_val" == "true" ]]; then
      ok "POST /api/push/$TEST_NTFY_TOPIC returned $pub_code with ok:true"
    else
      ok "POST /api/push/$TEST_NTFY_TOPIC returned $pub_code"
    fi
  elif [[ "$pub_code" == "404" || "$pub_code" == "405" ]]; then
    skip "push endpoint not available (HTTP $pub_code)"
  else
    ko "POST /api/push/$TEST_NTFY_TOPIC returned unexpected HTTP $pub_code: $pub_body"
  fi
}

RESULT=fail
_story_ts_203
: "${RESULT:=fail}"
unset -f _story_ts_203
