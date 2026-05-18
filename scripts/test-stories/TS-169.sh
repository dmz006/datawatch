#!/usr/bin/env bash
# TS-169 — Isolated stats shows separate uptime
# tags: surface:docker feature:bootstrap
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-169"
story_preflight "surface:docker feature:bootstrap" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
