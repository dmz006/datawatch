#!/usr/bin/env bash
# TS-290 — guardrail_library_list + guardrail_profile CRUD via MCP
# tags: surface:mcp feature:mcp feature:automata
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-290"
story_preflight "surface:mcp feature:mcp feature:automata" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
