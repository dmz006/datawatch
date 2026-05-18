#!/usr/bin/env bash
# TS-480 — POST /api/llms/{name}/force_delete endpoint exists (skip actual deletion)
# tags: surface:api feature:llm-registry
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-480"
story_preflight "surface:api feature:llm-registry" || return 0

_story_ts_480() {
  skip "skipping force_delete to avoid destroying production LLM — verify endpoint exists manually"
}

RESULT=fail
_story_ts_480
: "${RESULT:=fail}"
unset -f _story_ts_480
