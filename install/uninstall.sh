#!/usr/bin/env bash
set -euo pipefail

PURGE=false
for arg in "$@"; do
  [[ $arg == "--purge" ]] && PURGE=true
done

echo "Stopping and disabling claude-signal service..."

# System service
if systemctl is-active --quiet claude-signal 2>/dev/null; then
  sudo systemctl stop claude-signal
fi
if systemctl is-enabled --quiet claude-signal 2>/dev/null; then
  sudo systemctl disable claude-signal
fi
sudo rm -f /etc/systemd/system/claude-signal.service
sudo systemctl daemon-reload 2>/dev/null || true

# User service
if systemctl --user is-active --quiet claude-signal 2>/dev/null; then
  systemctl --user stop claude-signal
fi
if systemctl --user is-enabled --quiet claude-signal 2>/dev/null; then
  systemctl --user disable claude-signal
fi
rm -f "${HOME}/.config/systemd/user/claude-signal.service"
systemctl --user daemon-reload 2>/dev/null || true

# Remove binaries
sudo rm -f /usr/local/bin/claude-signal
rm -f "${HOME}/.local/bin/claude-signal"

echo "claude-signal removed."

if $PURGE; then
  echo ""
  echo "WARNING: --purge will delete all config, sessions, and logs."
  read -rp "Delete /etc/claude-signal, /var/lib/claude-signal, ~/.claude-signal? [y/N] " confirm
  if [[ "${confirm,,}" == "y" ]]; then
    sudo rm -rf /etc/claude-signal /var/lib/claude-signal /var/log/claude-signal
    rm -rf "${HOME}/.claude-signal"
    echo "Purge complete."
  fi
fi
