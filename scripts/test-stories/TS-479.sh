#!/usr/bin/env bash
# TS-479 — POST /api/llms/{name}/reassign returns 200 with count field
# tags: surface:api feature:llm-registry
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-479"
story_preflight "surface:api feature:llm-registry" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
