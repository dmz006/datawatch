#!/usr/bin/env bash
# TS-551 — GET /api/council/config returns config shape with llm_ref field
# tags: surface:api feature:council
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-551"
story_preflight "surface:api feature:council" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
