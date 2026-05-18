#!/usr/bin/env bash
# TS-365 — POST /api/sessions/{id}/input sends text with Enter
# tags: surface:api feature:sessions
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-365"
story_preflight "surface:api feature:sessions" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
