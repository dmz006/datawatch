#!/usr/bin/env bash
# TS-772 — web_search stats: GET /api/web_search/stats returns config
# tags: surface:api feature:web_search parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-772"

story_preflight "surface:api feature:web_search" || exit 0

# GET web search stats.
stats_raw=$(curl "${curl_args[@]}" "$TEST_TLS/api/web_search/stats" 2>/dev/null)
stats_code=$(curl "${curl_args[@]}" -o /dev/null -w "%{http_code}" "$TEST_TLS/api/web_search/stats" 2>/dev/null)
echo "  [TS-772] HTTP $stats_code stats=$stats_raw"
save_evidence "$CURRENT_STORY" "stats.json" "$stats_raw"

if [[ "$stats_code" != "200" ]]; then
  ko "GET /api/web_search/stats returned HTTP $stats_code (expected 200)"
  exit 0
fi

ws_enabled=$(echo "$stats_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('enabled','?'))" 2>/dev/null)
ws_engine=$(echo "$stats_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('engine','?'))" 2>/dev/null)
ws_provider=$(echo "$stats_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('provider','?'))" 2>/dev/null)
echo "  [TS-772] enabled=$ws_enabled engine=$ws_engine provider=$ws_provider"

if [[ -z "$ws_enabled" || "$ws_enabled" == "?" ]]; then
  ko "web_search/stats missing 'enabled' field: $stats_raw"
  exit 0
fi

ok "GET /api/web_search/stats: enabled=$ws_enabled engine=$ws_engine provider=$ws_provider"
