#!/usr/bin/env bash
# TS-323 — datawatch pipeline list exits 0
# tags: surface:cli feature:cli feature:parity
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-323"
story_preflight "surface:cli feature:cli feature:parity" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
