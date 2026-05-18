#!/usr/bin/env bash
# TS-278 — cooldown_status + cooldown_set + cooldown_clear via MCP
# tags: surface:mcp feature:mcp feature:config
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-278"
story_preflight "surface:mcp feature:mcp feature:config" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
