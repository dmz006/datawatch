#!/usr/bin/env bash
# export-ssh-pubkey.sh — export SSH public key to datawatch secrets
#
# Exports an SSH public key to the secrets manager for use in test environments,
# enabling test agents to authenticate via SSH without hardcoding keys.
#
# Usage:
#   ./export-ssh-pubkey.sh --key-path=$HOME/.ssh/id_rsa.pub --target-daemon=http://localhost:18080 --token=dw-test-token-12345
#   ./export-ssh-pubkey.sh -k ~/.ssh/id_ed25519.pub -d https://localhost:18443 -t "my-bearer-token"
#

set -uo pipefail

# Parse arguments
KEY_PATH=""
TARGET_DAEMON=""
BEARER_TOKEN=""
KEY_NAME="${KEY_NAME:-ssh-test-pubkey}"

for arg in "$@"; do
  case "$arg" in
    --key-path=*)      KEY_PATH="${arg#--key-path=}" ;;
    -k)                shift; KEY_PATH="$1" ;;
    --target-daemon=*) TARGET_DAEMON="${arg#--target-daemon=}" ;;
    -d)                shift; TARGET_DAEMON="$1" ;;
    --token=*)         BEARER_TOKEN="${arg#--token=}" ;;
    -t)                shift; BEARER_TOKEN="$1" ;;
    --name=*)          KEY_NAME="${arg#--name=}" ;;
    -n)                shift; KEY_NAME="$1" ;;
    --help|-h)
      echo "Usage: $0 --key-path=<path> --target-daemon=<url> --token=<bearer-token>"
      echo "  --key-path, -k             Path to SSH public key file (required, .pub)"
      echo "  --target-daemon, -d        Target datawatch daemon URL (required)"
      echo "  --token, -t                Bearer token for authentication (required)"
      echo "  --name, -n                 Secret name (default: ssh-test-pubkey)"
      echo ""
      echo "Example:"
      echo "  $0 --key-path=\$HOME/.ssh/id_rsa.pub --target-daemon=http://localhost:18080 --token=dw-test"
      exit 0
      ;;
  esac
done

# Validate required arguments
if [[ -z "$KEY_PATH" ]]; then
  echo "ERROR: --key-path is required"
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

# Expand ~ to home directory
KEY_PATH="${KEY_PATH/\~/$HOME}"

# Validate key file exists
if [[ ! -f "$KEY_PATH" ]]; then
  echo "ERROR: key file not found: $KEY_PATH"
  exit 1
fi

# Validate it's a public key (should have .pub extension or contain "PUBLIC KEY")
if [[ ! "$KEY_PATH" =~ \.pub$ ]] && ! head -1 "$KEY_PATH" | grep -q "PUBLIC KEY"; then
  echo "WARNING: file doesn't appear to be a public key"
fi

# Read the key
KEY_CONTENT=$(cat "$KEY_PATH")

echo "Exporting SSH public key to $TARGET_DAEMON"
echo "  Key file: $KEY_PATH"
echo "  Key type: $(head -1 "$KEY_PATH" | cut -d' ' -f1)"
echo "  Secret name: $KEY_NAME"
echo ""

# Store in secrets
echo "POSTing to $TARGET_DAEMON/api/secrets ..."

RESPONSE=$(curl -sk \
  -X POST \
  -H "Authorization: Bearer $BEARER_TOKEN" \
  -H "Content-Type: application/json" \
  -d @- \
  "${TARGET_DAEMON}/api/secrets" <<EOF
{
  "name": "$KEY_NAME",
  "value": "$(printf '%s' "$KEY_CONTENT" | sed 's/"/\\"/g')",
  "tags": "test,ssh,public-key",
  "scopes": "test:*",
  "description": "SSH public key for test agents - $(date -u +%FT%TZ)"
}
EOF
)

HTTP_CODE=$(echo "$RESPONSE" | tail -1 | grep -o '[0-9]\+' | head -1)
if [[ "$HTTP_CODE" == "200" ]] || [[ "$HTTP_CODE" == "201" ]]; then
  echo "✓ Successfully exported public key: $KEY_NAME"
  echo ""
  echo "Usage in test code:"
  echo ""
  echo "  # Retrieve public key from secrets"
  echo "  pubkey=\$(api GET /api/secrets/$KEY_NAME | jq -r '.value')"
  echo ""
  echo "  # Use for SSH authentication in test agents"
  echo "  mkdir -p ~/.ssh"
  echo "  echo \"\$pubkey\" >> ~/.ssh/authorized_keys"
  echo ""
  exit 0
else
  echo "ERROR: failed to export key (HTTP $HTTP_CODE)"
  echo "$RESPONSE"
  exit 1
fi
