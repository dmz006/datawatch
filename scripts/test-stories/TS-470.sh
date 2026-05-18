#!/usr/bin/env bash
# TS-470 — YAML autonomous.planning_backend is accepted by config reload
# tags: surface:locale feature:automata
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-470"
story_preflight "surface:locale feature:automata" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
