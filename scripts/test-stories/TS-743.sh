#!/usr/bin/env bash
# TS-743 — decomposition_profile vs backend separation: SET_LLM stores both independently
# tags: surface:api feature:automata parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-743"

story_preflight || exit 0

# Create a PRD
prd_body=$(api POST /api/autonomous/prds '{"spec":"test spec for TS-743 backend separation","backend":"claude-code","actor":"ts-743"}')
prd_id=$(echo "$prd_body" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null)
if [[ -z "$prd_id" ]]; then
  ko "Could not create PRD: $prd_body"
  finish_story; exit 0
fi

# Set the decomposition_profile to something different from the execution backend
set_body=$(api POST "/api/autonomous/prds/${prd_id}/set_llm" '{"backend":"ollama","decomposition_profile":"claude-code","actor":"ts-743"}')
set_code=$(api_code POST "/api/autonomous/prds/${prd_id}/set_llm" '{"backend":"ollama","decomposition_profile":"claude-code","actor":"ts-743"}')

if [[ "$set_code" != "200" ]]; then
  ko "set_llm returned $set_code: $set_body"
  finish_story; exit 0
fi

# Fetch the PRD and verify both fields are stored independently
prd_get=$(api GET "/api/autonomous/prds/${prd_id}")
got_backend=$(echo "$prd_get" | python3 -c "import sys,json; print(json.load(sys.stdin).get('backend',''))" 2>/dev/null)
got_profile=$(echo "$prd_get" | python3 -c "import sys,json; print(json.load(sys.stdin).get('decomposition_profile',''))" 2>/dev/null)

if [[ "$got_backend" != "ollama" ]]; then
  ko "Expected backend=ollama, got: $got_backend (full: $prd_get)"
  finish_story; exit 0
fi
if [[ "$got_profile" != "claude-code" ]]; then
  ko "Expected decomposition_profile=claude-code, got: $got_profile (full: $prd_get)"
  finish_story; exit 0
fi

ok "PRD $prd_id: backend=$got_backend decomposition_profile=$got_profile (independent storage confirmed)"
save_evidence "ts743_prd.json" "$prd_get"
finish_story
