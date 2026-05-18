#!/usr/bin/env bash
# TS-392 — GET /api/alerts/aggregated returns array with server field per item
# tags: surface:api feature:multi-server
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-392"
story_preflight "surface:api feature:multi-server" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
