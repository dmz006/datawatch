#!/usr/bin/env bash
# TS-352 — docs_read \"cross-agent-memory\" returns content with exec_steps
# tags: surface:mcp feature:mcp feature:howto feature:memory
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-352"
story_preflight "surface:mcp feature:mcp feature:howto feature:memory" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
