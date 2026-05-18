#!/usr/bin/env bash
# TS-265 — GET /api/splash/logo 404 is acceptable
# tags: surface:api feature:bootstrap
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-265"
story_preflight "surface:api feature:bootstrap" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
