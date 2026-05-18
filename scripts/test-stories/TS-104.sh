#!/usr/bin/env bash
# TS-104 — !alert list via POST /api/test/message
# tags: surface:comms feature:comms
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-104"
story_preflight "surface:comms feature:comms" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
