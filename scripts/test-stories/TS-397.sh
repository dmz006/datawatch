#!/usr/bin/env bash
# TS-397 — GET /api/mcp/prompts returns 10 prompts with name+description+arguments
# tags: surface:api feature:mcp-prompts
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-397"
story_preflight "surface:api feature:mcp-prompts" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
