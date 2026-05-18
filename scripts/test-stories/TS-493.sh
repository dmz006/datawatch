#!/usr/bin/env bash
# TS-493 — docs_search \"llm in-use enabled models\" returns llm-registry.md in hits
# tags: surface:mcp feature:llm-registry feature:howto
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-493"
story_preflight "surface:mcp feature:llm-registry feature:howto" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
