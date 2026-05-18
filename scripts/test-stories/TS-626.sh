#!/usr/bin/env bash
# TS-626 — routing:k8s-sidecar returns 400 (not yet supported)
# tags: surface:api feature:routing group:routing-v8 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-626"
story_preflight "surface:api feature:routing group:routing-v8 parallel:ok" || return 0

_story_ts_626() {
  local payload resp code
  payload='{"name":"r626-k8s","kind":"ollama","address":"http://localhost:11434","routing":"k8s-sidecar"}'
  resp=$(api_code POST /api/compute/nodes "$payload")
  code=$(echo "$resp" | grep -o '__HTTP_CODE_[0-9]*__' | tr -d '_' | sed 's/HTTP_CODE_//')
  save_evidence TS-626 "create_k8s_sidecar.json" "$resp"

  # k8s-sidecar is not yet supported — expect non-2xx
  if [[ "$code" =~ ^[45] ]]; then
    ok "routing:k8s-sidecar correctly rejected with non-2xx (HTTP $code)"
  else
    ko "expected non-2xx for unsupported routing:k8s-sidecar, got $code"
  fi
}

RESULT=fail
_story_ts_626
: "${RESULT:=fail}"
unset -f _story_ts_626
