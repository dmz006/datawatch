#!/usr/bin/env bash
# TS-499 — autonomous_type_list MCP tool returns 4 built-in types (software/research/operational/personal)
# tags: surface:mcp feature:automata
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-499"
story_preflight "surface:mcp feature:automata" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
