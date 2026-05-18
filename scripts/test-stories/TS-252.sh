#!/usr/bin/env bash
# TS-252 — GET /api/docs returns Swagger HTML (200)
# tags: surface:api feature:bootstrap
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-252"
story_preflight "surface:api feature:bootstrap" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
