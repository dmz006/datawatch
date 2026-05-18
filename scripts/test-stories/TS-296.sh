#!/usr/bin/env bash
# TS-296 — pipeline_list + pipeline_start + pipeline_status shape via MCP
# tags: surface:mcp feature:mcp feature:parity
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-296"
story_preflight "surface:mcp feature:mcp feature:parity" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
