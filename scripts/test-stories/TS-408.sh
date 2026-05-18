#!/usr/bin/env bash
# TS-408 — POST /api/mcp/sample with trigger=morning_briefing returns ok:true or error:sampling not supported
# tags: surface:api feature:mcp-sampling
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-408"
story_preflight "surface:api feature:mcp-sampling" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
