#!/usr/bin/env bash
# TS-443 — datawatch session new --llm shell \"test\" exits 0 and prints \"Session started.\"
# tags: surface:cli feature:sessions feature:cli
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-443"
story_preflight "surface:cli feature:sessions feature:cli" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
