#!/usr/bin/env bash
# TS-415 — GET /api/llms returns {llms:[]} or array with llm entries
# tags: surface:api feature:llm-registry
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-415"
story_preflight "surface:api feature:llm-registry" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
