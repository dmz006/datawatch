#!/usr/bin/env bash
# TS-401 — PUT /api/dashboard/layout round-trips (save + reload preserves cards)
# tags: surface:api feature:dashboard
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-401"
story_preflight "surface:api feature:dashboard" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
