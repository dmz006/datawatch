#!/usr/bin/env bash
# TS-420 — GET /api/migration/compute-kinds returns {nodes:[],supported:[]} shape
# tags: surface:api feature:compute
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-420"
story_preflight "surface:api feature:compute" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
