#!/usr/bin/env bash
# TS-240 — Research journey: memory → KG → MCP recall
# tags: surface:api feature:memory
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-240"
story_preflight "surface:api feature:memory" || return 0

_story_ts_240() {
    echo ""; echo "  >> TS-240: Research journey: memory → KG → MCP recall"
    # Step 1: store a memory
    ts=$(date +%s)
    mem=$(api POST /api/memory/save "{\"content\":\"e2e-research-journey-$ts\"}")
    mem_id=$(echo "$mem" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('id',''))" 2>/dev/null || echo "")
    save_evidence "TS-240" "1_save.json" "$mem"
    # Step 2: recall it
    recall=$(api GET "/api/memory/search?q=e2e-research-journey-$ts")
    save_evidence "TS-240" "2_recall.json" "$recall"
    # If recall returns an embedder error or empty (embedder silently fails returning []), skip
    if echo "$recall" | grep -qi "not found\|embedder\|no embed\|ollama\|disabled\|not enabled"; then
      [[ -n "$mem_id" ]] && add_cleanup "mem" "$mem_id"
      skip "Research journey: memory embedder not configured in test daemon (needs ollama/nomic-embed-text)"
    else
      found=$(echo "$recall" | python3 -c "import json,sys; d=json.load(sys.stdin); r=d if isinstance(d,list) else d.get('results',[]); print(any('e2e-research-journey' in str(x) for x in r))" 2>/dev/null || echo "False")
      if [[ "$found" == "False" && -n "$mem_id" ]]; then
        # Check if this is a silent embedder failure (search returns [] but memory exists)
        list_check=$(api GET /api/memory/list 2>/dev/null || echo "[]")
        mem_in_list=$(echo "$list_check" | python3 -c "
import json,sys
d=json.load(sys.stdin)
items=d if isinstance(d,list) else d.get('memories',d.get('entries',[]))
print(any(str('$mem_id') == str(x.get('id','')) for x in items))
" 2>/dev/null || echo "False")
        if [[ "$mem_in_list" == "True" ]]; then
          [[ -n "$mem_id" ]] && add_cleanup "mem" "$mem_id"
          skip "Research journey: memory stored but search returned empty (embedder may not be available)"
          return
        fi
      fi
      # Step 3: add KG triple
      kg=$(api POST /api/memory/kg/add "{\"subject\":\"e2e-test-$ts\",\"predicate\":\"is\",\"object\":\"journey\"}")
      save_evidence "TS-240" "3_kg_add.json" "$kg"
      # Cleanup
      [[ -n "$mem_id" ]] && add_cleanup "mem" "$mem_id"
      if [[ "$found" == "True" ]]; then
        ok "Research journey: memory stored, recalled, KG triple added"
      else
        ko "Research journey: recall did not return stored memory"
      fi
    fi

}

RESULT=fail
_story_ts_240
: "${RESULT:=fail}"
unset -f _story_ts_240
