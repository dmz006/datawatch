#!/usr/bin/env bash
# TS-510 — compute_node_list MCP tool returns nodes array
# tags: surface:mcp feature:compute
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-510"
story_preflight "surface:mcp feature:compute" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
