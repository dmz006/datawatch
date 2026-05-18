#!/usr/bin/env bash
# TS-228 — Howto: channel-state-engine
# tags: surface:api feature:howto feature:sessions
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-228"
story_preflight "surface:api feature:howto feature:sessions" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
