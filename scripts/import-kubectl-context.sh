#!/usr/bin/env bash
# import-kubectl-context.sh — export Kubernetes context and import to datawatch secrets
#
# Exports the specified kubectl context to JSON format, then POSTs it to the target
# datawatch daemon's /api/secrets endpoint for use in K8s integration tests.
#
# Usage:
#   ./import-kubectl-context.sh --context=testing --target-daemon=http://localhost:18080 --token=dw-test-token-12345
#   ./import-kubectl-context.sh -c testing -d https://localhost:18443 -t "my-bearer-token"
#
# Environment:
#   KUBECONFIG  - path to kubeconfig file (default: ~/.kube/config)
#

set -uo pipefail

# Parse arguments
CONTEXT=""
TARGET_DAEMON=""
BEARER_TOKEN=""
KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/config}"

for arg in "$@"; do
  case "$arg" in
    --context=*)      CONTEXT="${arg#--context=}" ;;
    -c)               shift; CONTEXT="$1" ;;
    --target-daemon=*) TARGET_DAEMON="${arg#--target-daemon=}" ;;
    -d)               shift; TARGET_DAEMON="$1" ;;
    --token=*)         BEARER_TOKEN="${arg#--token=}" ;;
    -t)               shift; BEARER_TOKEN="$1" ;;
    --help|-h)
      echo "Usage: $0 --context=<name> --target-daemon=<url> --token=<bearer-token>"
      echo "  --context, -c              Kubernetes context name (default: current context)"
      echo "  --target-daemon, -d        Target datawatch daemon URL (required)"
      echo "  --token, -t                Bearer token for authentication (required)"
      echo ""
      echo "Environment:"
      echo "  KUBECONFIG  Path to kubeconfig file (default: ~/.kube/config)"
      exit 0
      ;;
  esac
done

# Validate required arguments
if [[ -z "$TARGET_DAEMON" ]]; then
  echo "ERROR: --target-daemon is required"
  exit 1
fi

if [[ -z "$BEARER_TOKEN" ]]; then
  echo "ERROR: --token is required"
  exit 1
fi

# If no context specified, use current context
if [[ -z "$CONTEXT" ]]; then
  CONTEXT=$(kubectl config current-context) || CONTEXT=""
  if [[ -z "$CONTEXT" ]]; then
    echo "ERROR: no context specified and could not determine current context"
    exit 1
  fi
  echo "Using current kubectl context: $CONTEXT"
fi

# Verify context exists
if ! kubectl config get-contexts "$CONTEXT" &>/dev/null; then
  echo "ERROR: context '$CONTEXT' not found in kubeconfig"
  exit 1
fi

echo "Exporting kubectl context: $CONTEXT"

# Export context to JSON
# kubectl config view --flatten returns the full kubeconfig with all credentials
KUBECONFIG_JSON=$(kubectl config view --context="$CONTEXT" --flatten --raw 2>/dev/null | python3 -c '
import json, sys, os
try:
  import yaml
  data = yaml.safe_load(sys.stdin)
  print(json.dumps(data))
except:
  sys.stderr.write("ERROR: failed to parse kubeconfig (requires PyYAML)\n")
  sys.exit(1)
')

if [[ -z "$KUBECONFIG_JSON" ]]; then
  echo "ERROR: failed to export kubeconfig"
  exit 1
fi

echo "Kubeconfig exported ($(echo "$KUBECONFIG_JSON" | wc -c) bytes)"

# POST to target daemon
SECRET_NAME="k8s-context-${CONTEXT}"
echo ""
echo "POSTing to $TARGET_DAEMON/api/secrets ..."

RESPONSE=$(curl -sk \
  -X POST \
  -H "Authorization: Bearer $BEARER_TOKEN" \
  -H "Content-Type: application/json" \
  -d @- \
  "${TARGET_DAEMON}/api/secrets" <<EOF
{
  "name": "$SECRET_NAME",
  "value": $(echo "$KUBECONFIG_JSON" | python3 -c 'import json, sys; print(json.dumps(json.load(sys.stdin)))'),
  "tags": "test,k8s,kubectl",
  "scopes": "test:*",
  "description": "Kubectl context $CONTEXT exported $(date -u +%FT%TZ)"
}
EOF
)

HTTP_CODE=$(echo "$RESPONSE" | tail -1 | grep -o '[0-9]\+' | head -1)
if [[ "$HTTP_CODE" == "200" ]] || [[ "$HTTP_CODE" == "201" ]]; then
  echo "✓ Successfully stored context in secrets: $SECRET_NAME"
  echo ""
  echo "Usage in tests:"
  echo "  KUBECONFIG='/tmp/kubeconfig-test'"
  echo "  api GET /api/secrets/k8s-context-$CONTEXT > \"\$KUBECONFIG\""
  echo "  export KUBECONFIG  # kubectl will use this file"
  exit 0
else
  echo "ERROR: failed to store secret (HTTP $HTTP_CODE)"
  echo "$RESPONSE"
  exit 1
fi
