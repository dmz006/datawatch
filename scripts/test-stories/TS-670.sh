#!/usr/bin/env bash
# TS-670 — POST /api/council/run with image_path prepends description to proposals
# tags: surface:live feature:vision group:vision conflict:ollama-llava
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-670"
story_preflight "surface:live feature:vision group:vision conflict:ollama-llava" || return 0

_story_ts_670() {
  # Enable vision on sandbox daemon
  local cfg_code
  cfg_code=$(api_code PUT /api/config '{"vision.enabled":true,"vision.backend":"ollama","vision.model":"llava"}' | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ ! "$cfg_code" =~ ^2 ]]; then
    skip "could not enable vision on sandbox daemon (HTTP $cfg_code)"
    return
  fi

  # Write a tiny test PNG to a temp file the sandbox daemon can read
  local tmp_png="$RUN_DIR/council-test.png"
  printf '\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde\x00\x00\x00\x0cIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82' > "$tmp_png"

  # Check council is enabled
  local council_check
  council_check=$(api_code GET /api/council/config | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ "$council_check" == "503" || "$council_check" == "404" ]]; then
    skip "council endpoint not available (HTTP $council_check)"
    return
  fi

  local resp code body
  resp=$(api_code POST /api/council/run \
    "{\"proposal\":\"What is in the image?\",\"image_path\":\"$tmp_png\"}")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-670 "council_response.json" "$body"

  if [[ "$code" == "503" ]]; then
    skip "council returned 503 — no LLM backend or vision not reachable"
    return
  fi
  if [[ "$code" == "400" ]]; then
    ko "council rejected image_path: $(echo "$body" | head -c 200)"
    return
  fi
  if [[ ! "$code" =~ ^2 ]]; then
    ko "POST /api/council/run with image_path returned HTTP $code"
    return
  fi

  if echo "$body" | grep -q "\[image:"; then
    ok "council response contains [image: ...] — description was prepended to proposals"
  else
    ko "council response did not contain [image: ...]; vision injection may not have fired"
  fi
}

RESULT=fail
_story_ts_670
: "${RESULT:=fail}"
unset -f _story_ts_670
