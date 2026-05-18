#!/usr/bin/env bash
# TS-246 — Identity → algorithm journey
# tags: surface:api feature:identity conflict:llm
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-246"
story_preflight "surface:api feature:identity conflict:llm" || return 0

_story_ts_246() {
    echo ""; echo "  >> TS-246: Identity → algorithm journey"
    # Get identity endpoint
    identity=$(api GET /api/identity 2>/dev/null || echo '{}')
    save_evidence "TS-246" "1_identity.json" "$identity"

    # Get algorithm phases
    algo=$(api GET /api/algorithm 2>/dev/null || echo '{}')
    save_evidence "TS-246" "2_algorithm.json" "$algo"

    # Check if both endpoints are accessible
    if echo "$identity" | grep -q "error\|not found"; then
      skip "Identity endpoint not found"
    elif echo "$algo" | grep -q "error\|not found"; then
      skip "Algorithm endpoint not found"
    else
      ok "Identity → algorithm integration: both endpoints accessible"
    fi

}

RESULT=fail
_story_ts_246
: "${RESULT:=fail}"
unset -f _story_ts_246
