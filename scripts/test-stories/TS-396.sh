#!/usr/bin/env bash
# TS-396 — server_list MCP tool returns array
# tags: surface:mcp feature:multi-server
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-396"
story_preflight "surface:mcp feature:multi-server" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
