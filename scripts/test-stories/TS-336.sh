#!/usr/bin/env bash
# TS-336 — datawatch filter list exits 0
# tags: surface:cli feature:cli feature:filters
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-336"
story_preflight "surface:cli feature:cli feature:filters" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
