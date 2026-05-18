#!/usr/bin/env bash
# TS-442 — start_session MCP tool with llm param returns session with llm_ref set
# tags: surface:mcp feature:sessions
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-442"
story_preflight "surface:mcp feature:sessions" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
