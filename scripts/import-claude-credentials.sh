#!/usr/bin/env bash
# import-claude-credentials.sh — import Claude API key to datawatch secrets
#
# Stores Claude API key in the secrets manager for use in test environments,
# enabling test daemons to spawn sessions with claude-code backend.
#
# Usage:
#   ./import-claude-credentials.sh --api-key=$ANTHROPIC_API_KEY --target-daemon=http://localhost:18080 --token=dw-test-token-12345
#   ./import-claude-credentials.sh -k "sk-ant-..." -d https://localhost:18443 -t "my-bearer-token"
#   ANTHROPIC_API_KEY="sk-ant-..." ./import-claude-credentials.sh -d http://localhost:18080 -t "token"
#

set -uo pipefail

# Parse arguments
API_KEY=""
TARGET_DAEMON=""
BEARER_TOKEN=""
MODEL="${MODEL:-claude-haiku-4-5-20251001}"
EFFORT="${EFFORT:-quick}"

for arg in "$@"; do
  case "$arg" in
    --api-key=*)       API_KEY="${arg#--api-key=}" ;;
    -k)                shift; API_KEY="$1" ;;
    --target-daemon=*) TARGET_DAEMON="${arg#--target-daemon=}" ;;
    -d)                shift; TARGET_DAEMON="$1" ;;
    --token=*)         BEARER_TOKEN="${arg#--token=}" ;;
    -t)                shift; BEARER_TOKEN="$1" ;;
    --model=*)         MODEL="${arg#--model=}" ;;
    -m)                shift; MODEL="$1" ;;
    --effort=*)        EFFORT="${arg#--effort=}" ;;
    --help|-h)
      echo "Usage: $0 --api-key=<key> --target-daemon=<url> --token=<bearer-token>"
      echo "  --api-key, -k              Claude API key (or ANTHROPIC_API_KEY env)"
      echo "  --target-daemon, -d        Target datawatch daemon URL (required)"
      echo "  --token, -t                Bearer token for authentication (required)"
      echo "  --model, -m                Model to use (default: claude-haiku-4-5-20251001)"
      echo "  --effort                   Effort level for Claude (default: quick)"
      echo ""
      echo "Environment:"
      echo "  ANTHROPIC_API_KEY  Claude API key"
      echo "  MODEL              Claude model (overrides --model)"
      echo "  EFFORT             Effort level (overrides --effort)"
      exit 0
      ;;
  esac
done

# Use env var if api-key not provided
if [[ -z "$API_KEY" ]]; then
  API_KEY="${ANTHROPIC_API_KEY:-}"
fi

# Validate required arguments
if [[ -z "$API_KEY" ]]; then
  echo "ERROR: --api-key or ANTHROPIC_API_KEY env required"
  exit 1
fi

if [[ -z "$TARGET_DAEMON" ]]; then
  echo "ERROR: --target-daemon is required"
  exit 1
fi

if [[ -z "$BEARER_TOKEN" ]]; then
  echo "ERROR: --token is required"
  exit 1
fi

# Validate API key format (should start with sk-ant-)
if [[ ! "$API_KEY" =~ ^sk-ant- ]]; then
  echo "WARNING: API key doesn't match expected format (sk-ant-*)"
fi

echo "Importing Claude credentials to $TARGET_DAEMON"
echo "  Model: $MODEL"
echo "  Effort: $EFFORT"
echo ""

# Store API key in secrets
SECRET_NAME="claude-test-api-key"
echo "POSTing to $TARGET_DAEMON/api/secrets ..."

RESPONSE=$(curl -sk \
  -X POST \
  -H "Authorization: Bearer $BEARER_TOKEN" \
  -H "Content-Type: application/json" \
  -d @- \
  "${TARGET_DAEMON}/api/secrets" <<EOF
{
  "name": "$SECRET_NAME",
  "value": "$API_KEY",
  "tags": "test,claude,api-key",
  "scopes": "test:*",
  "description": "Claude API key for test daemon - $(date -u +%FT%TZ)"
}
EOF
)

HTTP_CODE=$(echo "$RESPONSE" | tail -1 | grep -o '[0-9]\+' | head -1)
if [[ "$HTTP_CODE" == "200" ]] || [[ "$HTTP_CODE" == "201" ]]; then
  echo "✓ Successfully stored API key in secrets: $SECRET_NAME"
  echo ""
  echo "Usage in test daemon config.yaml:"
  echo ""
  echo "  claude:"
  echo "    enabled: true"
  echo "    api_key_ref: \${secret:$SECRET_NAME}"
  echo "    model: $MODEL"
  echo "    default_effort: $EFFORT"
  echo ""
  exit 0
else
  echo "ERROR: failed to store secret (HTTP $HTTP_CODE)"
  echo "$RESPONSE"
  exit 1
fi
