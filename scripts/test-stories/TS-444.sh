#!/usr/bin/env bash
# TS-444 — datawatch session new --llm ollama --compute datawatch-ollama exits 0 and prints ComputeNode line
# tags: surface:cli feature:sessions feature:compute feature:cli
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-444"
story_preflight "surface:cli feature:sessions feature:compute feature:cli" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
