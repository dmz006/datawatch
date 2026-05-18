#!/usr/bin/env bash
# TS-402 — POST /api/sessions/{id}/hook-event accepts PostToolUse payload
# tags: surface:api feature:sessions feature:hooks
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-402"
story_preflight "surface:api feature:sessions feature:hooks" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
