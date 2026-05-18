#!/usr/bin/env bash
# TS-451 — GET /api/observer/peers entries carry compute_node field (present, may be empty string)
# tags: surface:api feature:observer feature:compute
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-451"
story_preflight "surface:api feature:observer feature:compute" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
