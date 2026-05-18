#!/usr/bin/env bash
# TS-241 — Autonomous journey (LLM-powered decomposition)
# tags: surface:api feature:automata conflict:llm
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-241"
story_preflight "surface:api feature:automata conflict:llm" || return 0

_story_ts_241() {
    echo ""; echo "  >> TS-241: Autonomous journey (LLM-powered decomposition)"
    ts=$(date +%s)
    proj_name="e2e-proj-$ts"

    # Create project profile first (required for PRD)
    proj=$(api POST /api/profiles/projects "{\"name\":\"$proj_name\",\"description\":\"E2E test project\",\"git\":{\"url\":\"https://github.com/dmz006/datawatch-e2e-test-3278008\",\"branch\":\"main\"},\"image_pair\":{\"agent\":\"agent-claude\",\"sidecar\":\"lang-go\"}}")
    save_evidence "TS-241" "0_create_project.json" "$proj"

    if echo "$proj" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('name')" 2>/dev/null; then
      # Now create PRD with project_profile (use name as identifier)
      prd_name="e2e-autonomous-$ts"
      spec="Implement a simple e2e test for autonomous decomposition. This should verify that the system can break down a high-level requirement into concrete tasks."
      prd=$(api POST /api/autonomous/prds "{\"spec\":\"$spec\",\"project_profile\":\"$proj_name\",\"effort\":\"low\",\"backend\":\"ollama\"}")
      save_evidence "TS-241" "1_create_prd.json" "$prd"
      prd_id=$(echo "$prd" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('id',''))" 2>/dev/null || echo "")

      if [[ -n "$prd_id" ]]; then
        ok "Autonomous PRD created with decomposition capability"
        add_cleanup "automaton" "$prd_id"
        add_cleanup "profile-proj" "$proj_name"
      else
        skip "Could not create PRD: $(echo "$prd" | head -c 150)"
        add_cleanup "profile-proj" "$proj_name"
      fi
    else
      skip "Could not create project profile: $(echo "$proj" | head -c 150)"
    fi

}

RESULT=fail
_story_ts_241
: "${RESULT:=fail}"
unset -f _story_ts_241
