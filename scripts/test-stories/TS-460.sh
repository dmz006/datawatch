#!/usr/bin/env bash
# TS-460 — compute_node_attach_observer MCP tool sets observer_peer field
# tags: surface:mcp feature:observer feature:compute
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-460"
story_preflight "surface:mcp feature:observer feature:compute" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
