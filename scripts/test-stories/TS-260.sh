#!/usr/bin/env bash
# TS-260 — GET /api/orchestrator/verdicts returns {verdicts:[]} shape
# tags: surface:api feature:parity
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-260"
story_preflight "surface:api feature:parity" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
