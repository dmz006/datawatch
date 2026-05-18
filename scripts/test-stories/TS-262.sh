#!/usr/bin/env bash
# TS-262 — GET /api/templates returns array
# tags: surface:api feature:plugins
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-262"
story_preflight "surface:api feature:plugins" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
