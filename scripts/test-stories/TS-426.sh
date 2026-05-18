#!/usr/bin/env bash
# TS-426 — datawatch llm list exits 0
# tags: surface:cli feature:llm-registry feature:cli
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-426"
story_preflight "surface:cli feature:llm-registry feature:cli" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
