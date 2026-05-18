#!/usr/bin/env bash
# TS-519 — GET /api/compute/nodes/{name} response has auto_tags field separate from tags
# tags: surface:api feature:compute
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-519"
story_preflight "surface:api feature:compute" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
