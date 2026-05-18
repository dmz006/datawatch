#!/usr/bin/env bash
# TS-466 — autonomous_prd_decompose MCP tool accepts planning_backend parameter
# tags: surface:mcp feature:automata
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-466"
story_preflight "surface:mcp feature:automata" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
