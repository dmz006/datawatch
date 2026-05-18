#!/usr/bin/env bash
# TS-270 — algorithm_list via MCP returns array
# tags: surface:mcp feature:mcp feature:algorithm
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-270"
story_preflight "surface:mcp feature:mcp feature:algorithm" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
