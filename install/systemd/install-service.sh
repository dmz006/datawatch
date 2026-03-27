#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ $EUID -ne 0 ]]; then
  echo "This script must be run as root (or with sudo) to install a system service."
  echo "For a user-level service (no root), use the main installer: install/install.sh"
  exit 1
fi

cp "${SCRIPT_DIR}/datawatch.service" /etc/systemd/system/
systemctl daemon-reload
systemctl enable datawatch
echo "Service installed. Start with: sudo systemctl start datawatch"
