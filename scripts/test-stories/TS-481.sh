#!/usr/bin/env bash
# TS-481 — DELETE /api/llms/{name} returns 409 when active bindings exist
# tags: surface:api feature:llm-registry
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-481"
story_preflight "surface:api feature:llm-registry" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
