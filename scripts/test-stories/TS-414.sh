#!/usr/bin/env bash
# TS-414 — GET /api/observer/peers/by-node returns {by_node:{},unbound:[]} shape
# tags: surface:api feature:observer
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-414"
story_preflight "surface:api feature:observer" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
