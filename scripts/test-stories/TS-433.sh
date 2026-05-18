#!/usr/bin/env bash
# TS-433 — docs_search \"mcp sampling\" returns result referencing mcp-sampling howto
# tags: surface:mcp feature:mcp feature:howto feature:mcp-sampling
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-433"
story_preflight "surface:mcp feature:mcp feature:howto feature:mcp-sampling" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
