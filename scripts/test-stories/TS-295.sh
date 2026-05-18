#!/usr/bin/env bash
# TS-295 — orchestrator_config_get + orchestrator_graph_list + orchestrator_verdicts via MCP
# tags: surface:mcp feature:mcp feature:parity
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-295"
story_preflight "surface:mcp feature:mcp feature:parity" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
