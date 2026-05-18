#!/usr/bin/env bash
# TS-288 — eval_list_suites + eval_run smoke suite shape via MCP
# tags: surface:mcp feature:mcp feature:evals
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-288"
story_preflight "surface:mcp feature:mcp feature:evals" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
