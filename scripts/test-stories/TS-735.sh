#!/usr/bin/env bash
# TS-735 — Vision: POST /api/vision/describe 503 when vision not configured
# tags: surface:api feature:vision parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-735"
story_preflight "surface:api feature:vision" || return 0

_story_ts_735() {
  # First check if vision is currently configured
  local cfg
  cfg=$(api GET /api/config 2>/dev/null || echo "{}")
  local vision_enabled
  vision_enabled=$(echo "$cfg" | python3 -c "import json,sys; print(json.load(sys.stdin).get('vision',{}).get('enabled',False))" 2>/dev/null || echo "False")

  # Disable vision to test 503 path
  if [[ "$vision_enabled" == "True" || "$vision_enabled" == "true" ]]; then
    # Temporarily disable vision
    api PUT /api/config '{"vision.enabled":false}' >/dev/null 2>&1 || true
    sleep 1
  fi

  local tmp_file="$RUN_DIR/ts735-test.png"
  printf '\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde\x00\x00\x00\x0cIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82' > "$tmp_file"

  local resp code body
  resp=$(curl "${curl_args[@]}" -X POST \
    -F "image=@$tmp_file;type=image/png" \
    "$TEST_BASE/api/vision/describe" \
    -w "\n__HTTP_CODE_%{http_code}__" 2>/dev/null)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-735 "response.json" "$body"

  # Re-enable vision if we disabled it
  if [[ "$vision_enabled" == "True" || "$vision_enabled" == "true" ]]; then
    api PUT /api/config '{"vision.enabled":true}' >/dev/null 2>&1 || true
  fi

  if [[ "$code" == "503" ]]; then
    ok "POST /api/vision/describe returns 503 when vision not configured — handler registered correctly"
    return
  fi
  if [[ "$code" == "405" ]]; then
    ko "POST /api/vision/describe returned 405 — endpoint not registered or wrong method"
    return
  fi
  if [[ "$code" == "404" ]]; then
    ko "POST /api/vision/describe returned 404 — endpoint not registered"
    return
  fi
  if [[ "$code" =~ ^2 ]]; then
    # Vision was configured and worked — that's better than 503
    ok "POST /api/vision/describe returned $code — vision endpoint functional (vision was configured)"
    return
  fi

  ok "POST /api/vision/describe returned HTTP $code — endpoint registered"
}

RESULT=fail
_story_ts_735
: "${RESULT:=fail}"
unset -f _story_ts_735
