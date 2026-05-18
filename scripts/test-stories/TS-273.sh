#!/usr/bin/env bash
# TS-273 — autonomous_status via MCP returns {enabled,...} shape
# tags: surface:mcp feature:mcp feature:automata
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-273"
story_preflight "surface:mcp feature:mcp feature:automata" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
