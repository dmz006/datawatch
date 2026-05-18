#!/usr/bin/env bash
# TS-409 — POST /api/mcp/elicit with schema=approval returns form shape or error:elicitation not supported
# tags: surface:api feature:mcp-elicitation
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-409"
story_preflight "surface:api feature:mcp-elicitation" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
