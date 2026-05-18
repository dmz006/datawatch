#!/usr/bin/env bash
# TS-471 — docs_search \"autonomous planning prd-plan\" returns autonomous-planning.md
# tags: surface:mcp feature:automata feature:howto
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-471"
story_preflight "surface:mcp feature:automata feature:howto" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
