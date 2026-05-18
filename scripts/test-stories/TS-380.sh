#!/usr/bin/env bash
# TS-380 — POST /api/autonomous/prds/{id}/decompose respects effort timeout (high→15min)
# tags: surface:api feature:automata conflict:llm
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-380"
story_preflight "surface:api feature:automata conflict:llm" || return 0

_story_ts_380() {
  # Requires a live LLM and running PRD — skip in standard test environment
  skip "requires live LLM backend and running PRD — skip in standard test env"
}

RESULT=fail
_story_ts_380
: "${RESULT:=fail}"
unset -f _story_ts_380
