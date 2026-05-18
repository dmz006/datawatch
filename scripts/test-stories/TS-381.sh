#!/usr/bin/env bash
# TS-381 — GET /api/push/<topic> streams SSE events (ntfy-compat)
# tags: surface:api feature:push
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-381"
story_preflight "surface:api feature:push" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
