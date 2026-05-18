#!/usr/bin/env bash
# TS-513 — GET /.well-known/unifiedpush returns discovery document shape
# tags: surface:api feature:push
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-513"
story_preflight "surface:api feature:push" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
