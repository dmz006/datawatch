#!/usr/bin/env bash
# TS-388 — GET /api/servers/{name} returns single entry; 404 on unknown
# tags: surface:api feature:multi-server
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-388"
story_preflight "surface:api feature:multi-server" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
