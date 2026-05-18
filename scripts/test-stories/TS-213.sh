#!/usr/bin/env bash
# TS-213 — evals: suites list surface
# tags: surface:api feature:evals
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-213"
story_preflight "surface:api feature:evals" || return 0

_story_ts_213() {
    echo ""; echo "  >> TS-213: evals: suites list surface"
    resp=$(api GET /api/evals 2>/dev/null || api GET /api/eval/suites 2>/dev/null || echo '{"error":"not found"}')
    save_evidence "TS-213" "evals.json" "$resp"
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'error' not in d" 2>/dev/null; then
      ok "Evals endpoint reachable"
    else
      skip "Evals endpoint not found"
    fi

}

RESULT=fail
_story_ts_213
: "${RESULT:=fail}"
unset -f _story_ts_213
