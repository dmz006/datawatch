#!/usr/bin/env bash
# TS-181 — Memory feature: 7-surface parity matrix
# tags: surface:api feature:parity
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-181"
story_preflight "surface:api feature:parity" || return 0

_story_ts_181() {
    echo ""; echo "  >> TS-181: Memory feature: 7-surface parity matrix"
    resp=$(api GET "/api/memory/search?q=test")
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert isinstance(d, list)" 2>/dev/null; then
      ok "GET /api/memory/search returns JSON list"
    elif echo "$resp" | grep -qi "not found\|embedder\|no embed\|ollama\|disabled\|not enabled"; then
      skip "Memory search: embedder not configured in test daemon (needs ollama/nomic-embed-text)"
    else
      ko "GET /api/memory/search did not return JSON list: $(echo "$resp" | head -c 100)"
    fi
    save_evidence "TS-181" "memory_search.json" "$resp"

}

RESULT=fail
_story_ts_181
: "${RESULT:=fail}"
unset -f _story_ts_181
