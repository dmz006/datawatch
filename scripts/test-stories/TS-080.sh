#!/usr/bin/env bash
# TS-080 — POST /api/mcp/elicit structured response
# tags: surface:mcp feature:mcp
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-080"
story_preflight "surface:mcp feature:mcp" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
