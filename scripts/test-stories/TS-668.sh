#!/usr/bin/env bash
# TS-668 — comms image injection: session output contains [image: ...] prefix
# tags: surface:live feature:vision group:vision conflict:ollama-llava
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-668"
story_preflight "surface:live feature:vision group:vision conflict:ollama-llava" || return 0

_story_ts_668() {
  # Enable vision on sandbox daemon
  local cfg_code
  cfg_code=$(api_code PUT /api/config '{"vision.enabled":true,"vision.backend":"ollama","vision.model":"llmvision/glimpse-v1:latest"}' | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ ! "$cfg_code" =~ ^2 ]]; then
    skip "could not enable vision on sandbox daemon (HTTP $cfg_code)"
    return
  fi

  # Check webhook comms is running (it's in testdata config)
  local webhook_resp
  webhook_resp=$(api_code POST /api/comms/webhook/inbound \
    '{"text":"describe this","attachments":[{"type":"image","url":"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="}]}' 2>/dev/null || echo "__HTTP_CODE_000__")
  local wh_code
  wh_code=$(echo "$webhook_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')

  if [[ "$wh_code" == "000" || "$wh_code" == "404" || "$wh_code" == "503" ]]; then
    skip "webhook comms not available for image injection test (HTTP $wh_code)"
    return
  fi

  # Give the session a moment to process the image
  sleep 3

  # Find the most recent session and check its output for [image:
  local sessions sess_id output
  sessions=$(api GET /api/sessions 2>/dev/null || echo "[]")
  sess_id=$(echo "$sessions" | python3 -c "import json,sys; s=json.load(sys.stdin); print(s[0]['id'] if s else '')" 2>/dev/null || true)

  if [[ -z "$sess_id" ]]; then
    skip "no sessions found after webhook delivery"
    return
  fi

  output=$(api GET "/api/sessions/$sess_id/output" 2>/dev/null || echo "")
  save_evidence TS-668 "session_output.txt" "$output"

  if echo "$output" | grep -q "\[image:"; then
    ok "session output contains [image: ...] injection from vision backend"
  else
    ko "session output did not contain [image: ...] — vision injection may have failed"
  fi
}

RESULT=fail
_story_ts_668
: "${RESULT:=fail}"
unset -f _story_ts_668
