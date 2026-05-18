#!/usr/bin/env bash
# TS-555 — docs_list_howtos returns at least 30 howto paths
# tags: surface:mcp feature:howto
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-555"
story_preflight "surface:mcp feature:howto" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"
