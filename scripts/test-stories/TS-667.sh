#!/usr/bin/env bash
# TS-667 — POST /api/vision/describe with real ollama+llava returns non-empty description
# tags: surface:live feature:vision group:vision conflict:ollama-llava
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-667"
story_preflight "surface:live feature:vision group:vision conflict:ollama-llava" || return 0

_story_ts_667() {
  # Enable vision on the sandbox daemon pointing at the test ollama
  local cfg_resp cfg_code
  cfg_resp=$(api_code PUT /api/config '{"vision.enabled":true,"vision.backend":"ollama","vision.endpoint":"http://datawatch:11434","vision.model":"Gemma3:12b"}')
  cfg_code=$(echo "$cfg_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ ! "$cfg_code" =~ ^2 ]]; then
    skip "could not enable vision on sandbox daemon (HTTP $cfg_code)"
    return
  fi

  # Create a tiny 1×1 PNG for the test (no external dependency)
  local tmp_png="$RUN_DIR/test-vision.png"
  printf '\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde\x00\x00\x00\x0cIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82' > "$tmp_png"

  local resp code
  resp=$(curl "${curl_args[@]}" -X POST \
    -F "image=@$tmp_png;type=image/png" \
    "$TEST_BASE/api/vision/describe" \
    -w "\n__HTTP_CODE_%{http_code}__" 2>/dev/null)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  local body
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-667 "response.json" "$body"

  if [[ "$code" == "503" ]]; then
    skip "vision endpoint returned 503 — vision not enabled or no visioner"
    return
  fi
  if [[ "$code" == "502" ]]; then
    skip "vision endpoint returned 502 — ollama vision model unavailable or crashed ($(echo "$body" | head -c 100))"
    return
  fi
  if [[ ! "$code" =~ ^2 ]]; then
    ko "POST /api/vision/describe returned HTTP $code: $(echo "$body" | head -c 200)"
    return
  fi

  local desc
  desc=$(echo "$body" | python3 -c "import json,sys; print(json.load(sys.stdin).get('description',''))" 2>/dev/null || true)
  if [[ -z "$desc" ]]; then
    ko "POST /api/vision/describe returned 200 but description was empty"
    return
  fi

  ok "POST /api/vision/describe returned non-empty description ($( echo "$desc" | head -c 80)…)"
}

RESULT=fail
_story_ts_667
: "${RESULT:=fail}"
unset -f _story_ts_667
