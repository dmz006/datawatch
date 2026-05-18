#!/usr/bin/env bash
# TS-394 — datawatch server list exits 0
# tags: surface:cli feature:multi-server feature:cli
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-394"
story_preflight "surface:cli feature:multi-server feature:cli" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
