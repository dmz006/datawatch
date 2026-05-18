#!/usr/bin/env bash
# TS-378 — GET /api/evals returns {runs:[{id,name,status,score,created_at}]} shape
# tags: surface:api feature:evals
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-378"
story_preflight "surface:api feature:evals" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
