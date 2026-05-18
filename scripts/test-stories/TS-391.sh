#!/usr/bin/env bash
# TS-391 — POST /api/servers/{name}/test returns {ok:true} for live local server
# tags: surface:api feature:multi-server
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-391"
story_preflight "surface:api feature:multi-server" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
