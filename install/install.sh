#!/usr/bin/env bash
# claude-signal installer for Linux
# Supports: Ubuntu, Debian, RHEL, CentOS, Fedora, Arch, openSUSE
# Runs with or without root. Non-root installs to ~/.local/bin and uses user systemd.
set -euo pipefail

VERSION="0.1.0"
REPO="dmz006/claude-signal"
SIGNAL_CLI_VERSION="0.13.4"
BINARY_NAME="claude-signal"

# Colors
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info()    { echo -e "${BLUE}[INFO]${NC} $*"; }
success() { echo -e "${GREEN}[OK]${NC} $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
error()   { echo -e "${RED}[ERROR]${NC} $*" >&2; exit 1; }

# Parse flags
ROOT_INSTALL=false
SKIP_DEPS=false
INSTALL_SERVICE=false
HELP=false

for arg in "$@"; do
  case $arg in
    --root)   ROOT_INSTALL=true ;;
    --skip-deps) SKIP_DEPS=true ;;
    --service) INSTALL_SERVICE=true ;;
    --help|-h) HELP=true ;;
  esac
done

if $HELP; then
  cat <<EOF
claude-signal installer

Usage: ./install.sh [OPTIONS]

Options:
  --root        Install system-wide (requires sudo). Default: install for current user.
  --skip-deps   Skip installing Java/tmux dependencies.
  --service     Install and enable as a systemd service.
  --help        Show this help.

Non-root install (default):
  Binary:   ~/.local/bin/claude-signal
  Data:     ~/.claude-signal/
  Service:  ~/.config/systemd/user/claude-signal.service

Root install (--root):
  Binary:   /usr/local/bin/claude-signal
  Data:     /var/lib/claude-signal/
  Config:   /etc/claude-signal/
  Service:  /etc/systemd/system/claude-signal.service
EOF
  exit 0
fi

# Detect if we have sudo
HAS_SUDO=false
if command -v sudo &>/dev/null && sudo -n true 2>/dev/null; then
  HAS_SUDO=true
fi

SUDO=""
if $ROOT_INSTALL; then
  if [[ $EUID -ne 0 ]]; then
    if $HAS_SUDO; then
      SUDO="sudo"
    else
      error "Root install requires sudo. Run as root or omit --root for user install."
    fi
  fi
fi

# Detect distro
detect_distro() {
  if [[ -f /etc/os-release ]]; then
    . /etc/os-release
    echo "${ID:-unknown}"
  else
    echo "unknown"
  fi
}

detect_pkg_manager() {
  if command -v apt-get &>/dev/null; then echo "apt"
  elif command -v dnf &>/dev/null; then echo "dnf"
  elif command -v yum &>/dev/null; then echo "yum"
  elif command -v pacman &>/dev/null; then echo "pacman"
  elif command -v zypper &>/dev/null; then echo "zypper"
  else echo "unknown"
  fi
}

DISTRO=$(detect_distro)
PKG_MGR=$(detect_pkg_manager)

info "Detected distro: ${DISTRO}, package manager: ${PKG_MGR}"

# Install system dependencies
install_deps() {
  if $SKIP_DEPS; then
    warn "Skipping dependency installation."
    return
  fi

  info "Installing dependencies: Java 17+, tmux..."

  case $PKG_MGR in
    apt)
      $SUDO apt-get update -qq
      $SUDO apt-get install -y openjdk-17-jdk-headless tmux curl wget
      ;;
    dnf)
      $SUDO dnf install -y java-17-openjdk-headless tmux curl wget
      ;;
    yum)
      $SUDO yum install -y java-17-openjdk-headless tmux curl wget
      ;;
    pacman)
      $SUDO pacman -Sy --noconfirm jdk17-openjdk tmux curl wget
      ;;
    zypper)
      $SUDO zypper install -y java-17-openjdk-headless tmux curl wget
      ;;
    *)
      warn "Cannot auto-install deps for pkg manager: ${PKG_MGR}. Install Java 17+ and tmux manually."
      ;;
  esac

  success "Dependencies installed."
}

# Install signal-cli
install_signal_cli() {
  if command -v signal-cli &>/dev/null; then
    EXISTING=$(signal-cli --version 2>/dev/null | head -1 || echo "unknown")
    info "signal-cli already installed: ${EXISTING}. Skipping."
    return
  fi

  info "Installing signal-cli ${SIGNAL_CLI_VERSION}..."

  TMPDIR=$(mktemp -d)
  trap 'rm -rf "$TMPDIR"' EXIT

  TARBALL="signal-cli-${SIGNAL_CLI_VERSION}.tar.gz"
  URL="https://github.com/AsamK/signal-cli/releases/download/v${SIGNAL_CLI_VERSION}/${TARBALL}"

  wget -q --show-progress -O "${TMPDIR}/${TARBALL}" "${URL}" || \
    curl -fsSL -o "${TMPDIR}/${TARBALL}" "${URL}"

  if $ROOT_INSTALL; then
    $SUDO tar -xzf "${TMPDIR}/${TARBALL}" -C /opt/
    $SUDO ln -sf "/opt/signal-cli-${SIGNAL_CLI_VERSION}/bin/signal-cli" /usr/local/bin/signal-cli
  else
    mkdir -p "${HOME}/.local/opt"
    tar -xzf "${TMPDIR}/${TARBALL}" -C "${HOME}/.local/opt/"
    mkdir -p "${HOME}/.local/bin"
    ln -sf "${HOME}/.local/opt/signal-cli-${SIGNAL_CLI_VERSION}/bin/signal-cli" "${HOME}/.local/bin/signal-cli"
  fi

  success "signal-cli installed."
}

# Install claude-signal binary
install_binary() {
  info "Installing claude-signal binary..."

  if $ROOT_INSTALL; then
    INSTALL_DIR="/usr/local/bin"
  else
    INSTALL_DIR="${HOME}/.local/bin"
    mkdir -p "${INSTALL_DIR}"
  fi

  # Try to build from source if Go is available
  if command -v go &>/dev/null; then
    info "Go found. Building from source..."
    TMPBUILD=$(mktemp -d)
    cd "${TMPBUILD}"
    git clone --depth 1 "https://github.com/${REPO}.git" .
    go build -ldflags="-X main.Version=${VERSION}" -o "${INSTALL_DIR}/${BINARY_NAME}" ./cmd/claude-signal/
    cd - >/dev/null
    rm -rf "${TMPBUILD}"
    success "Built and installed from source."
    return
  fi

  # Try to download prebuilt binary
  ARCH=$(uname -m)
  case $ARCH in
    x86_64)  GOARCH="amd64" ;;
    aarch64) GOARCH="arm64" ;;
    armv7l)  GOARCH="arm"   ;;
    *)       warn "Unknown arch: ${ARCH}. Download binary manually from https://github.com/${REPO}/releases"; return ;;
  esac

  BIN_URL="https://github.com/${REPO}/releases/download/v${VERSION}/${BINARY_NAME}-linux-${GOARCH}"
  wget -q --show-progress -O "${INSTALL_DIR}/${BINARY_NAME}" "${BIN_URL}" || \
    curl -fsSL -o "${INSTALL_DIR}/${BINARY_NAME}" "${BIN_URL}"
  chmod +x "${INSTALL_DIR}/${BINARY_NAME}"

  success "Binary installed to ${INSTALL_DIR}/${BINARY_NAME}."
}

# Create directories
create_dirs() {
  info "Creating data directories..."
  if $ROOT_INSTALL; then
    $SUDO mkdir -p /etc/claude-signal /var/lib/claude-signal /var/log/claude-signal
    # Create system user if it doesn't exist
    if ! id claude-signal &>/dev/null; then
      $SUDO useradd --system --no-create-home --shell /usr/sbin/nologin \
        --home-dir /var/lib/claude-signal claude-signal
    fi
    $SUDO chown -R claude-signal:claude-signal /var/lib/claude-signal /var/log/claude-signal /etc/claude-signal
  else
    mkdir -p "${HOME}/.claude-signal" "${HOME}/.local/share/signal-cli"
  fi
  success "Directories created."
}

# Install systemd service
install_service() {
  if ! command -v systemctl &>/dev/null; then
    warn "systemd not found. Skipping service installation."
    return
  fi

  if $ROOT_INSTALL; then
    info "Installing system-wide systemd service..."
    $SUDO cp "$(dirname "$0")/systemd/claude-signal.service" /etc/systemd/system/
    $SUDO systemctl daemon-reload
    $SUDO systemctl enable claude-signal
    success "System service installed. Start with: sudo systemctl start claude-signal"
  else
    info "Installing user systemd service..."
    SERVICE_DIR="${HOME}/.config/systemd/user"
    mkdir -p "${SERVICE_DIR}"

    # Write user service file
    cat > "${SERVICE_DIR}/claude-signal.service" <<EOF
[Unit]
Description=Claude Signal - Signal to Claude Code Bridge
After=network-online.target default.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${HOME}/.local/bin/claude-signal start
ExecReload=/bin/kill -HUP \$MAINPID
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=claude-signal
Environment=HOME=${HOME}
Environment=PATH=${HOME}/.local/bin:/usr/local/bin:/usr/bin:/bin

[Install]
WantedBy=default.target
EOF

    systemctl --user daemon-reload
    systemctl --user enable claude-signal
    success "User service installed. Start with: systemctl --user start claude-signal"
    info "Enable lingering so service starts at boot (without login): loginctl enable-linger ${USER}"
  fi
}

# Check PATH
check_path() {
  if ! $ROOT_INSTALL; then
    if [[ ":$PATH:" != *":${HOME}/.local/bin:"* ]]; then
      warn "${HOME}/.local/bin is not in your PATH."
      warn "Add this to ~/.bashrc or ~/.zshrc:"
      warn '  export PATH="$HOME/.local/bin:$PATH"'
    fi
  fi
}

# Print next steps
next_steps() {
  echo ""
  echo -e "${GREEN}=== Installation Complete ===${NC}"
  echo ""
  echo "Next steps:"
  echo "  1. Link your Signal account:"
  echo "       claude-signal link"
  echo ""
  echo "  2. Create a Signal group with yourself on your phone."
  echo "     Then get the group ID:"
  echo "       signal-cli -u +1XXXXXXXXXX listGroups"
  echo ""
  echo "  3. Configure claude-signal:"
  echo "       claude-signal config init"
  echo ""
  if $INSTALL_SERVICE; then
    if $ROOT_INSTALL; then
      echo "  4. Start the service:"
      echo "       sudo systemctl start claude-signal"
    else
      echo "  4. Start the service:"
      echo "       systemctl --user start claude-signal"
    fi
  else
    echo "  4. Start the daemon:"
    echo "       claude-signal start"
  fi
  echo ""
  echo "  5. Send 'help' in your Signal group to verify."
  echo ""
  echo "Full documentation: https://github.com/${REPO}"
}

# Main
main() {
  info "claude-signal installer v${VERSION}"
  if $ROOT_INSTALL; then
    info "Mode: system-wide install"
  else
    info "Mode: user install (no root required)"
  fi

  install_deps
  install_signal_cli
  install_binary
  create_dirs

  if $INSTALL_SERVICE; then
    install_service
  fi

  check_path
  next_steps
}

main
