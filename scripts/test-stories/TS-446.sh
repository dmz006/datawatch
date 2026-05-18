#!/usr/bin/env bash
# TS-446 — comm new:llm=claude-code:<task> creates session with llm_ref set (checked via REST)
# tags: surface:comm feature:sessions
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-446"
story_preflight "surface:comm feature:sessions" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
