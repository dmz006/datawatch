#!/usr/bin/env bash
# TS-293 — memory_scope_recall + memory_scope_borrow + memory_scope_seed via MCP
# tags: surface:mcp feature:mcp feature:memory
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-293"
story_preflight "surface:mcp feature:mcp feature:memory" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
