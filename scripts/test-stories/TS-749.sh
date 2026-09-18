#!/usr/bin/env bash
# TS-749 — PWA Settings: web_search section visible and editable
# tags: surface:pwa feature:mcp-search conflict:pwa parallel:no
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-749"
story_preflight "surface:pwa feature:mcp-search conflict:pwa" || return 0
run_pwa_story "TS-749"
