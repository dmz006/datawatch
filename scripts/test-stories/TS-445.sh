#!/usr/bin/env bash
# TS-445 — GET /api/sessions response for CLI-created session has backend_family field matching LLM kind
# tags: surface:cli surface:api feature:sessions feature:cli
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-445"
story_preflight "surface:cli surface:api feature:sessions feature:cli" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
