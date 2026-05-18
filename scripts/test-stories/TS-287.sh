#!/usr/bin/env bash
# TS-287 — docs_apply for curated howto exec_steps executes via MCP
# tags: surface:mcp feature:mcp feature:howto
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-287"
story_preflight "surface:mcp feature:mcp feature:howto" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
