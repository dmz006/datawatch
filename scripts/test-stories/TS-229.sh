#!/usr/bin/env bash
# TS-229 — Howto: voice-input
# tags: surface:api feature:howto
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-229"
story_preflight "surface:api feature:howto" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
