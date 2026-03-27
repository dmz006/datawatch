#!/usr/bin/env bash
set -euo pipefail

PURGE=false
for arg in "$@"; do
  [[ $arg == "--purge" ]] && PURGE=true
done

echo "Stopping and disabling datawatch service..."

# System service
if systemctl is-active --quiet datawatch 2>/dev/null; then
  sudo systemctl stop datawatch
fi
if systemctl is-enabled --quiet datawatch 2>/dev/null; then
  sudo systemctl disable datawatch
fi
sudo rm -f /etc/systemd/system/datawatch.service
sudo systemctl daemon-reload 2>/dev/null || true

# User service
if systemctl --user is-active --quiet datawatch 2>/dev/null; then
  systemctl --user stop datawatch
fi
if systemctl --user is-enabled --quiet datawatch 2>/dev/null; then
  systemctl --user disable datawatch
fi
rm -f "${HOME}/.config/systemd/user/datawatch.service"
systemctl --user daemon-reload 2>/dev/null || true

# Remove binaries
sudo rm -f /usr/local/bin/datawatch
rm -f "${HOME}/.local/bin/datawatch"

echo "datawatch removed."

if $PURGE; then
  echo ""
  echo "WARNING: --purge will delete all config, sessions, and logs."
  read -rp "Delete /etc/datawatch, /var/lib/datawatch, ~/.datawatch? [y/N] " confirm
  if [[ "${confirm,,}" == "y" ]]; then
    sudo rm -rf /etc/datawatch /var/lib/datawatch /var/log/datawatch
    rm -rf "${HOME}/.datawatch"
    echo "Purge complete."
  fi
fi
