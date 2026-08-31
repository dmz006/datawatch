#!/usr/bin/env bash
# TS-669 — skill with accepts_images:true receives image context injected into task text
# tags: surface:live feature:vision group:vision conflict:ollama-llava
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-669"
story_preflight "surface:live feature:vision group:vision conflict:ollama-llava" || return 0

_story_ts_669() {
  # Enable vision on sandbox daemon
  local cfg_code
  cfg_code=$(api_code PUT /api/config '{"vision.enabled":true,"vision.backend":"ollama","vision.model":"llmvision/glimpse-v1:latest"}' | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ ! "$cfg_code" =~ ^2 ]]; then
    skip "could not enable vision on sandbox daemon (HTTP $cfg_code)"
    return
  fi

  # Check if any skill declares accepts_images: true
  local skills_resp skills_with_images
  skills_resp=$(api GET /api/skills 2>/dev/null || echo "[]")
  skills_with_images=$(echo "$skills_resp" | python3 -c "
import json,sys
skills = json.load(sys.stdin)
ai_skills = [s.get('name','?') for s in skills if s.get('accepts_images')]
print('\n'.join(ai_skills))
" 2>/dev/null || true)

  if [[ -z "$skills_with_images" ]]; then
    skip "no skills with accepts_images:true found in sandbox daemon; create one to run this test"
    return
  fi

  # This test requires manual verification: confirm the skill received [image: ...] in task text.
  # Automated verification requires a skill that echoes its task text, which is environment-specific.
  skip "TS-669 requires a skill with accepts_images:true to echo received task text — run manually"
}

RESULT=fail
_story_ts_669
: "${RESULT:=fail}"
unset -f _story_ts_669
