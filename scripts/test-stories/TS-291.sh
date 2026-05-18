#!/usr/bin/env bash
# TS-291 — llm_list + llm_get + llm_enable/disable round-trip via MCP
# tags: surface:mcp feature:mcp feature:config
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-291"
story_preflight "surface:mcp feature:mcp feature:config" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
