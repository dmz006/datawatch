#!/usr/bin/env bash
# TS-547 — docs_search \"memory scope hierarchy borrow\" returns cross-agent-memory.md
# tags: surface:mcp feature:memory feature:howto
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-547"
story_preflight "surface:mcp feature:memory feature:howto" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
