#!/usr/bin/env bash
# TS-434 — docs_list_howtos contains dashboard and compute-nodes and mcp-sampling
# tags: surface:mcp feature:mcp feature:howto
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-434"
story_preflight "surface:mcp feature:mcp feature:howto" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
