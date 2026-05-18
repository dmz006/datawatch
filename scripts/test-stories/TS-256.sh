#!/usr/bin/env bash
# TS-256 — POST /api/devices/register shape round-trip
# tags: surface:api feature:config
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-256"
story_preflight "surface:api feature:config" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
