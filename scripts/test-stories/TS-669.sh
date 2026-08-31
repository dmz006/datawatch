#!/usr/bin/env bash
# TS-669 — skill with accepts_images:true is reachable via GET /api/skills after registry sync
# tags: surface:live feature:vision group:vision conflict:ollama-llava
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-669"
story_preflight "surface:live feature:vision group:vision conflict:ollama-llava" || return 0

_story_ts_669() {
  # Enable vision on sandbox daemon pointing at the datawatch ollama instance
  local cfg_code
  cfg_code=$(api_code PUT /api/config \
    '{"vision.enabled":true,"vision.backend":"ollama","vision.endpoint":"http://datawatch:11434","vision.model":"Gemma3:12b"}' \
    | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ ! "$cfg_code" =~ ^2 ]]; then
    skip "could not enable vision on sandbox daemon (HTTP $cfg_code)"
    return
  fi

  # Create a minimal local git repo acting as a skills registry
  local skill_repo="$RUN_DIR/test-skills-registry"
  mkdir -p "$skill_repo/skills/image-reviewer"

  cat > "$skill_repo/skills/image-reviewer/skill.yaml" << 'SKILL_YAML'
name: image-reviewer
description: Reviews screenshots, diagrams, and visual content using vision context.
version: "1.0.0"
entry: SKILL.md
accepts_images: true
author: datawatch-test
license: MIT
category: vision
datawatch_min_version: "8.15.0"
SKILL_YAML

  cat > "$skill_repo/skills/image-reviewer/SKILL.md" << 'SKILL_MD'
# Image Reviewer

You are a visual reviewer. When `[image: <description>]` appears in the task,
use that context to provide feedback on what is shown.
SKILL_MD

  git -C "$skill_repo" init -q 2>/dev/null
  git -C "$skill_repo" -c user.name=test -c user.email=test@test add . 2>/dev/null
  git -C "$skill_repo" -c user.name=test -c user.email=test@test commit -qm "test skill" 2>/dev/null
  local branch
  branch=$(git -C "$skill_repo" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "main")

  # Register the local repo as a skills registry in the sandbox daemon
  local reg_code
  reg_code=$(api_code POST /api/skills/registries \
    "{\"name\":\"test-vision\",\"url\":\"file://$skill_repo\",\"branch\":\"$branch\",\"enabled\":true}" \
    | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ ! "$reg_code" =~ ^2 ]]; then
    skip "could not create test skills registry (HTTP $reg_code) — skills subsystem may be disabled"
    return
  fi

  # Connect (clone/fetch) the registry
  local conn_code
  conn_code=$(api_code POST /api/skills/registries/test-vision/connect \
    | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ ! "$conn_code" =~ ^2 ]]; then
    skip "skills registry connect failed (HTTP $conn_code)"
    return
  fi

  # Sync the image-reviewer skill
  local sync_code
  sync_code=$(api_code POST /api/skills/registries/test-vision/sync \
    '{"skills":["image-reviewer"]}' \
    | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ ! "$sync_code" =~ ^2 ]]; then
    ko "skills sync failed (HTTP $sync_code)"
    return
  fi

  # Verify the skill appears with accepts_images: true
  local skills_resp skills_with_images
  skills_resp=$(api GET /api/skills 2>/dev/null || echo "[]")
  save_evidence TS-669 "skills_list.json" "$skills_resp"
  skills_with_images=$(echo "$skills_resp" | python3 -c "
import json,sys
skills = json.load(sys.stdin)
ai_skills = [s.get('name','?') for s in skills if s.get('accepts_images')]
print('\n'.join(ai_skills))
" 2>/dev/null || true)

  if [[ -z "$skills_with_images" ]]; then
    ko "image-reviewer synced but not visible in GET /api/skills with accepts_images:true"
    return
  fi

  ok "skill(s) with accepts_images:true available after registry sync: $skills_with_images"
}

RESULT=fail
_story_ts_669
: "${RESULT:=fail}"
unset -f _story_ts_669
