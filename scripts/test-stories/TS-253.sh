#!/usr/bin/env bash
# TS-253 — GET /api/cooldown returns {active, until} shape
# tags: surface:api feature:config
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-253"
story_preflight "surface:api feature:config" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
