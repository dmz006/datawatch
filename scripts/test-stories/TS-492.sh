#!/usr/bin/env bash
# TS-492 — 5 locale bundles contain llm_in_use_empty key
# tags: surface:locale feature:llm-registry
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-492"
story_preflight "surface:locale feature:llm-registry" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
