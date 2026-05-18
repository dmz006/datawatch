#!/usr/bin/env bash
# TS-398 — POST /api/mcp/prompts/get with name=diagnose-system returns messages array
# tags: surface:api feature:mcp-prompts
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-398"
story_preflight "surface:api feature:mcp-prompts" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
