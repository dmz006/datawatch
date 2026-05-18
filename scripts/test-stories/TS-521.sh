#!/usr/bin/env bash
# TS-521 — POST /api/memory/scopes/seed seeds a scope with initial memories
# tags: surface:api feature:memory
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-521"
story_preflight "surface:api feature:memory" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
