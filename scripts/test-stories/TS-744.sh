#!/usr/bin/env bash
# TS-744 — PWA Monitor tab: web search stats card visible when web_search.enabled
# tags: surface:pwa feature:mcp-search conflict:pwa parallel:no
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-744"
story_preflight "surface:pwa feature:mcp-search conflict:pwa" || return 0
run_pwa_story "TS-744"
