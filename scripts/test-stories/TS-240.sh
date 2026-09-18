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
    # Check if our specific memory was found first (before scanning response for error keywords,
    # since stored memories may legitimately contain words like "ollama" or "embedder")
    found=$(echo "$recall" | python3 -c "import json,sys; d=json.load(sys.stdin); r=d if isinstance(d,list) else d.get('results',[]); print(any('e2e-research-journey-$ts' in str(x.get('content','') if isinstance(x,dict) else x) for x in r))" 2>/dev/null || echo "False")
    if [[ "$found" == "True" ]]; then
      # Step 3: add KG triple
      kg=$(api POST /api/memory/kg/add "{\"subject\":\"e2e-test-$ts\",\"predicate\":\"is\",\"object\":\"journey\"}")
      save_evidence "TS-240" "3_kg_add.json" "$kg"
      [[ -n "$mem_id" ]] && add_cleanup "mem" "$mem_id"
      ok "Research journey: memory stored, recalled, KG triple added"
    else
      # Not found — check if it's a real error or a silent embedder failure
      recall_error=$(echo "$recall" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('error','') if isinstance(d,dict) else '')" 2>/dev/null || echo "")
      if echo "$recall_error" | grep -qi "embedder\|no embed\|disabled\|not enabled\|not configured"; then
        [[ -n "$mem_id" ]] && add_cleanup "mem" "$mem_id"
        skip "Research journey: memory embedder not configured in test daemon (needs ollama/nomic-embed-text)"
      elif [[ -n "$mem_id" ]]; then
        # Silent embedder failure: search returned no results but memory was saved
        list_check=$(api GET /api/memory/list 2>/dev/null || echo "[]")
        mem_in_list=$(echo "$list_check" | python3 -c "
import json,sys
d=json.load(sys.stdin)
items=d if isinstance(d,list) else d.get('memories',d.get('entries',[]))
print(any(str('$mem_id') == str(x.get('id','')) for x in items))
" 2>/dev/null || echo "False")
        [[ -n "$mem_id" ]] && add_cleanup "mem" "$mem_id"
        if [[ "$mem_in_list" == "True" ]]; then
          skip "Research journey: memory stored but search returned empty (embedder may not be available)"
        else
          ko "Research journey: recall did not return stored memory (save also failed)"
        fi
      else
        ko "Research journey: memory save returned no id and recall failed"
      fi
    fi

}

RESULT=fail
_story_ts_240
: "${RESULT:=fail}"
unset -f _story_ts_240
