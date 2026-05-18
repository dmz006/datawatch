#!/usr/bin/env bash
# TS-121 — datawatch mcp resources (v7.1)
# tags: surface:cli feature:mcp
# legacy fn: t10_ts121_mcp_resources_cli
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-121"
story_preflight "surface:cli feature:mcp" || return 0

_story_ts_121() {
  # MCP resources list is a v7.1+ feature — mark as skip
  skip "MCP resources CLI list deferred to v7.1 (BL302)"
}

RESULT=fail
_story_ts_121
: "${RESULT:=fail}"
unset -f _story_ts_121
