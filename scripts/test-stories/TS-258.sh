#!/usr/bin/env bash
# TS-258 — GET /api/marketplace/ollama/catalog returns catalog array
# tags: surface:api feature:parity
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-258"
story_preflight "surface:api feature:parity" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
