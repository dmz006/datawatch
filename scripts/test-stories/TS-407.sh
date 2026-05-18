#!/usr/bin/env bash
# TS-407 — GET /api/mcp/resources/templates returns array with uriTemplate field
# tags: surface:api feature:mcp-resources
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-407"
story_preflight "surface:api feature:mcp-resources" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
