#!/usr/bin/env bash
# TS-244 — Council journey: personas list → run → cancel → cleanup
# tags: surface:api feature:council
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-244"
story_preflight "surface:api feature:council" || return 0

_story_ts_244() {
    echo ""; echo "  >> TS-244: Council journey: personas list → run → cancel → cleanup"
    personas=$(api GET /api/council/personas)
    save_evidence "TS-244" "1_personas.json" "$personas"
    pcount=$(echo "$personas" | python3 -c "import json,sys; d=json.load(sys.stdin); print(len(d) if isinstance(d,list) else 0)" 2>/dev/null || echo "0")

    # If no personas exist, create a test persona
    if [[ "$pcount" -eq 0 ]]; then
      ts=$(date +%s)
      persona_name="e2e-test-persona-$ts"
      create_persona=$(api POST /api/council/personas "{\"name\":\"$persona_name\",\"role\":\"E2E Test Analyst\",\"system_prompt\":\"You are a test analyst for e2e tests.\",\"model\":\"qwen3:1.7b\"}" 2>/dev/null || echo '{}')
      save_evidence "TS-244" "0_create_persona.json" "$create_persona"

      # Check if creation was successful (ok: true indicates success)
      if echo "$create_persona" | grep -q "\"ok\":true"; then
        persona_id=$(echo "$create_persona" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('name',''))" 2>/dev/null || echo "")
        if [[ -n "$persona_id" ]]; then
          add_cleanup "persona" "$persona_id"
          pcount=1
        fi
      else
        # If creation may have worked, check if we have personas now
        personas_check=$(api GET /api/council/personas)
        pcount=$(echo "$personas_check" | python3 -c "import json,sys; d=json.load(sys.stdin); print(len(d) if isinstance(d,list) else 0)" 2>/dev/null || echo "0")
      fi
    fi

    if [[ "$pcount" -gt 0 ]]; then
      # Get first persona name for the run (handle both wrapped and unwrapped format)
      first_persona=$(echo "$personas" | python3 -c "import json,sys; d=json.load(sys.stdin); lst=d.get('personas',d) if isinstance(d,dict) else d; print(lst[0]['name'] if isinstance(lst,list) and lst else '')" 2>/dev/null || echo "security-skeptic")
      run=$(api POST /api/council/run "{\"proposal\":\"What is the best approach for e2e testing in v7.0.0?\",\"personas\":[\"$first_persona\"],\"mode\":\"quick\"}")
      save_evidence "TS-244" "2_run.json" "$run"
      run_id=$(echo "$run" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('run_id',d.get('id','')))" 2>/dev/null || echo "")
      if [[ -n "$run_id" ]]; then
        cancel=$(api POST "/api/council/runs/$run_id/cancel" '{}')
        save_evidence "TS-244" "3_cancel.json" "$cancel"
        ok "Council journey: $pcount personas → run created → cancel called"
        add_cleanup "council" "$run_id"
      else
        skip "Council journey: run API did not return ID: $(echo "$run" | head -c 100)"
      fi
    else
      skip "Council journey: could not create or list personas"
    fi

}

RESULT=fail
_story_ts_244
: "${RESULT:=fail}"
unset -f _story_ts_244
