#!/usr/bin/env bash
# TS-217 — Howto: skills-sync
# tags: surface:api feature:howto feature:skills
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-217"
story_preflight "surface:api feature:howto feature:skills" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
