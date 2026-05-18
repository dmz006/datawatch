#!/usr/bin/env bash
# TS-504 — GET /api/compute/nodes/{name}/detail returns 200 or 503 (never 500)
# tags: surface:api feature:compute
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-504"
story_preflight "surface:api feature:compute" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
