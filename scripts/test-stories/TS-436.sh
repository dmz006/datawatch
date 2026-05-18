#!/usr/bin/env bash
# TS-436 — POST /api/sessions/start with llm=claude-code sets llm_ref on returned session
# tags: surface:api feature:sessions
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-436"
story_preflight "surface:api feature:sessions" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
