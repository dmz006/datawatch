#!/usr/bin/env bash
# TS-400 — GET /api/dashboard/layout returns valid JSON shape
# tags: surface:api feature:dashboard
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-400"
story_preflight "surface:api feature:dashboard" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
