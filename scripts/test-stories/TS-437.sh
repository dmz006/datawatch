#!/usr/bin/env bash
# TS-437 — POST /api/sessions/start with llm+compute_node sets both llm_ref and compute_node_ref
# tags: surface:api feature:sessions feature:compute
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-437"
story_preflight "surface:api feature:sessions feature:compute" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
