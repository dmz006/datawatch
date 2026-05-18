#!/usr/bin/env bash
# TS-390 — DELETE /api/servers/{name} returns 200; GET returns 404
# tags: surface:api feature:multi-server
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-390"
story_preflight "surface:api feature:multi-server" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
