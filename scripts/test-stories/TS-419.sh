#!/usr/bin/env bash
# TS-419 — GET /api/marketplace/ollama/catalog returns array with name+tags fields
# tags: surface:api feature:compute
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-419"
story_preflight "surface:api feature:compute" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
