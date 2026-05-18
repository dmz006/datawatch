#!/usr/bin/env bash
# TS-380 — POST /api/autonomous/prds/{id}/decompose respects effort timeout (high→15min)
# tags: surface:api feature:automata conflict:llm
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-380"
story_preflight "surface:api feature:automata conflict:llm" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
