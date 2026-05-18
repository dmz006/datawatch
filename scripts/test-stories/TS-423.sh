#!/usr/bin/env bash
# TS-423 — POST /api/sessions/set_llm_ref updates session llm_ref binding
# tags: surface:api feature:sessions feature:llm-registry
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-423"
story_preflight "surface:api feature:sessions feature:llm-registry" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
