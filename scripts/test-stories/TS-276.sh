#!/usr/bin/env bash
# TS-276 — compute_node_list via MCP returns array
# tags: surface:mcp feature:mcp feature:compute
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-276"
story_preflight "surface:mcp feature:mcp feature:compute" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
