#!/usr/bin/env bash
# TS-106 — GET /api/commands list
# tags: surface:comms feature:comms
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-106"
story_preflight "surface:comms feature:comms" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
