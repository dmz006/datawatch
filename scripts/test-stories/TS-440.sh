#!/usr/bin/env bash
# TS-440 — GET /api/sessions response has backend_family field (not llm_backend)
# tags: surface:api feature:sessions
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-440"
story_preflight "surface:api feature:sessions" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
