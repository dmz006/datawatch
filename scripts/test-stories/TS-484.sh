#!/usr/bin/env bash
# TS-484 — llm_in_use MCP tool returns bindings shape
# tags: surface:mcp feature:llm-registry
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-484"
story_preflight "surface:mcp feature:llm-registry" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
