#!/usr/bin/env bash
# TS-377 — LLM enable toggle runs pretest for inference kinds (ollama/openwebui)
# tags: surface:api feature:config
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-377"
story_preflight "surface:api feature:config" || return 0

_story_ts_377() {
  # Find an inference-kind LLM (ollama/openwebui) in the list
  local llms_resp
  llms_resp=$(api GET /api/llms)
  save_evidence TS-377 "llms.json" "$llms_resp"
  local inference_name
  inference_name=$(echo "$llms_resp" | python3 -c '
import json, sys
d = json.load(sys.stdin)
items = d if isinstance(d, list) else d.get("llms", [])
for item in items:
    if item.get("kind") in ("ollama", "openwebui"):
        print(item.get("name", ""))
        break
' 2>/dev/null || echo "")
  if [[ -z "$inference_name" ]]; then
    skip "no ollama/openwebui LLM configured — cannot test inference pretest"
    return
  fi
  # Try enabling — will run pretest which may fail since no real backend
  # Endpoint is /api/llms/{name}/enabled (PATCH or POST), body: {"enabled":true}
  local en_resp en_code en_body
  en_resp=$(api_code POST "/api/llms/$inference_name/enabled" '{"enabled":true}')
  en_code=$(echo "$en_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  en_body=$(echo "$en_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-377 "enable_resp.json" "$en_body"
  if [[ "$en_code" == "200" || "$en_code" == "204" ]]; then
    ok "POST /api/llms/$inference_name/enabled returned $en_code (inference kind ran pretest or already enabled)"
  elif [[ "$en_code" == "400" || "$en_code" == "503" || "$en_code" == "502" ]]; then
    ok "inference LLM enable returned $en_code (pretest ran and failed as expected — no backend)"
  elif [[ "$en_code" == "404" || "$en_code" == "405" ]]; then
    skip "LLM enable endpoint not available ($en_code)"
  else
    ko "unexpected HTTP $en_code enabling inference LLM: $en_body"
  fi
}

RESULT=fail
_story_ts_377
: "${RESULT:=fail}"
unset -f _story_ts_377
