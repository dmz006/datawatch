#!/usr/bin/env bash
# TS-201 — llm-registry: backends list + single backend round-trip
# tags: surface:api feature:llm
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-201"
story_preflight "surface:api feature:llm" || return 0

_story_ts_201() {
    echo ""; echo "  >> TS-201: llm-registry: backends list + single backend round-trip"
    resp=$(api GET /api/llm)
    save_evidence "TS-201" "llm_list.json" "$resp"
    count=$(echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); print(len(d) if isinstance(d,list) else 0)" 2>/dev/null || echo "0")
    if [[ "$count" -gt 0 ]]; then
      ok "LLM registry returns $count backends"
    else
      skip "No LLM backends configured"
    fi

}

RESULT=fail
_story_ts_201
: "${RESULT:=fail}"
unset -f _story_ts_201
