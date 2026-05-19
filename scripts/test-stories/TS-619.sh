#!/usr/bin/env bash
# TS-619 — peer's /api/proxy/llm route reachable
# tags: surface:api feature:routing group:routing-v8 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-619"
story_preflight "surface:api feature:routing group:routing-v8 parallel:ok" || return 0

_story_ts_619() {
  local peer_url="${DW_PEER_URL:-}"
  local peer_token="${DW_PEER_TOKEN:-peer-test-token}"
  local peer_pid=""
  local peer_data=""

  # If DW_PEER_URL is not set, spin up an inline peer daemon.
  if [[ -z "$peer_url" ]]; then
    local binary="${TEST_BINARY:-$REPO_ROOT/bin/datawatch}"
    if [[ ! -x "$binary" ]]; then
      binary="$(command -v datawatch 2>/dev/null || true)"
    fi
    if [[ -z "$binary" ]]; then
      skip "no peer daemon URL (DW_PEER_URL) and no datawatch binary for inline peer"; return
    fi

    # Allocate two free ports for the peer (HTTP + TLS)
    local peer_http peer_tls
    peer_http=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); p=s.getsockname()[1]; s.close(); print(p)' 2>/dev/null || echo "")
    peer_tls=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); p=s.getsockname()[1]; s.close(); print(p)' 2>/dev/null || echo "")
    if [[ -z "$peer_http" || -z "$peer_tls" ]]; then
      skip "could not allocate free ports for inline peer daemon"; return
    fi

    peer_data=$(mktemp -d)

    # Write a minimal peer config
    cat > "$peer_data/config.yaml" <<PEERCFG
hostname: dw-e2e-peer
data_dir: ${peer_data}
server:
  host: 127.0.0.1
  port: ${peer_http}
  tls_enabled: true
  tls_auto_generate: true
  tls_port: ${peer_tls}
  token: "${peer_token}"
session:
  llm_backend: shell
  kill_sessions_on_exit: true
PEERCFG

    # Start the peer daemon
    "$binary" start --foreground --config "$peer_data/config.yaml" \
      >> "$peer_data/peer.log" 2>&1 &
    peer_pid=$!

    # Wait up to 15s for the peer to become healthy
    local deadline=$(( $(date +%s) + 15 ))
    local healthy=0
    while [[ $(date +%s) -lt $deadline ]]; do
      if curl -skf "https://127.0.0.1:$peer_tls/api/health" > /dev/null 2>&1 || \
         curl -sf  "http://127.0.0.1:$peer_http/api/health" > /dev/null 2>&1; then
        healthy=1
        break
      fi
      sleep 0.3
    done

    if [[ $healthy -eq 0 ]]; then
      kill "$peer_pid" 2>/dev/null || true
      rm -rf "$peer_data"
      skip "inline peer daemon did not start within 15s"; return
    fi

    peer_url="https://127.0.0.1:$peer_tls"
  fi

  # Probe the peer's /api/proxy/llm/test-llm endpoint
  local code
  code=$(curl -sk --max-time 10 \
    -H "Authorization: Bearer $peer_token" \
    -w "%{http_code}" -o /dev/null \
    "${peer_url}/api/proxy/llm/test-llm" 2>/dev/null || echo "000")
  save_evidence TS-619 "peer_probe_code.txt" "$code"

  # Clean up inline peer if we started one
  if [[ -n "$peer_pid" ]]; then
    kill "$peer_pid" 2>/dev/null || true
    wait "$peer_pid" 2>/dev/null || true
    rm -rf "$peer_data" 2>/dev/null || true
  fi

  if [[ "$code" == "000" ]]; then
    ko "peer /api/proxy/llm/test-llm unreachable (connection refused or timeout)"
  else
    ok "peer /api/proxy/llm route reachable (HTTP $code)"
  fi
}

RESULT=fail
_story_ts_619
: "${RESULT:=fail}"
unset -f _story_ts_619
