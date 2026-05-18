#!/usr/bin/env bash
# TS-485 — llm_refresh_models MCP tool returns 200
# tags: surface:mcp feature:llm-registry
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-485"
story_preflight "surface:mcp feature:llm-registry" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
