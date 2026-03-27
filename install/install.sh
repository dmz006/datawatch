#!/usr/bin/env bash
# datawatch installer for Linux
# Supports: Ubuntu, Debian, RHEL, CentOS, Fedora, Arch, openSUSE
# Runs with or without root. Non-root installs to ~/.local/bin and uses user systemd.
set -euo pipefail

REPO="dmz006/datawatch"
SIGNAL_CLI_VERSION="0.14.1"
BINARY_NAME="datawatch"

# Fetch the latest published release version from GitHub API.
# Falls back to a hardcoded minimum version if the API is unavailable.
fetch_latest_version() {
  local fallback="0.5.0"
  local api_url="https://api.github.com/repos/${REPO}/releases/latest"
  local ver=""
  if command -v curl &>/dev/null; then
    ver=$(curl -fsSL "${api_url}" 2>/dev/null \
          | grep '"tag_name"' | head -1 \
          | sed 's/.*"tag_name": *"v\?\([^"]*\)".*/\1/')
  elif command -v wget &>/dev/null; then
    ver=$(wget -qO- "${api_url}" 2>/dev/null \
          | grep '"tag_name"' | head -1 \
          | sed 's/.*"tag_name": *"v\?\([^"]*\)".*/\1/')
  fi
  if [[ -z "${ver}" ]]; then
    warn "Could not fetch latest version from GitHub; defaulting to v${fallback}"
    ver="${fallback}"
  fi
  echo "${ver}"
}

VERSION=$(fetch_latest_version)

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
datawatch installer

Usage: ./install.sh [OPTIONS]

Options:
  --root        Install system-wide (requires sudo). Default: install for current user.
  --skip-deps   Skip installing Java/tmux dependencies.
  --service     Install and enable as a systemd service.
  --help        Show this help.

Non-root install (default):
  Binary:   ~/.local/bin/datawatch
  Data:     ~/.datawatch/
  Service:  ~/.config/systemd/user/datawatch.service

Root install (--root):
  Binary:   /usr/local/bin/datawatch
  Data:     /var/lib/datawatch/
  Config:   /etc/datawatch/
  Service:  /etc/systemd/system/datawatch.service
EOF
  exit 0
fi

SUDO=""
if $ROOT_INSTALL; then
  if [[ $EUID -ne 0 ]]; then
    if command -v sudo &>/dev/null; then
      SUDO="sudo"
      # Pre-validate sudo (prompts for password now rather than mid-install)
      if ! sudo -v; then
        error "sudo authentication failed. Run as root or omit --root for a user install."
      fi
    else
      error "Root install requires sudo or running as root."$'\n'"To install without root, omit --root (installs to ~/.local/bin)."
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

  # Check if deps are already satisfied
  local need_java=true need_tmux=true
  if command -v java &>/dev/null; then
    local jver
    jver=$(java -version 2>&1 | awk -F '"' '/version/ {print $2}' | cut -d. -f1)
    [[ "${jver:-0}" -ge 17 ]] 2>/dev/null && need_java=false
  fi
  command -v tmux &>/dev/null && need_tmux=false

  if ! $need_java && ! $need_tmux; then
    success "Java 17+ and tmux already installed. Skipping."
    return
  fi

  info "Installing dependencies: Java 17+, tmux..."

  # Determine how to elevate privileges for package installation
  local pkg_sudo=""
  if [[ $EUID -ne 0 ]]; then
    if command -v sudo &>/dev/null; then
      pkg_sudo="sudo"
    else
      warn "Cannot install system packages: sudo not found and not running as root."
      warn "Install Java 17+ and tmux manually, then re-run with --skip-deps:"
      case $PKG_MGR in
        apt)    warn "  sudo apt-get install -y openjdk-17-jdk-headless tmux" ;;
        dnf)    warn "  sudo dnf install -y java-17-openjdk-headless tmux" ;;
        yum)    warn "  sudo yum install -y java-17-openjdk-headless tmux" ;;
        pacman) warn "  sudo pacman -Sy jdk17-openjdk tmux" ;;
        zypper) warn "  sudo zypper install -y java-17-openjdk-headless tmux" ;;
        *)      warn "  Install Java 17+ and tmux via your system package manager." ;;
      esac
      return 1
    fi
  fi

  case $PKG_MGR in
    apt)
      $pkg_sudo apt-get update -qq
      $pkg_sudo apt-get install -y openjdk-17-jdk-headless tmux curl wget
      ;;
    dnf)
      $pkg_sudo dnf install -y java-17-openjdk-headless tmux curl wget
      ;;
    yum)
      $pkg_sudo yum install -y java-17-openjdk-headless tmux curl wget
      ;;
    pacman)
      $pkg_sudo pacman -Sy --noconfirm jdk17-openjdk tmux curl wget
      ;;
    zypper)
      $pkg_sudo zypper install -y java-17-openjdk-headless tmux curl wget
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
    EXISTING=$(signal-cli --version 2>/dev/null | awk '{print $2}' | head -1 || echo "unknown")
    if [[ "${EXISTING}" == "${SIGNAL_CLI_VERSION}" ]]; then
      success "signal-cli ${SIGNAL_CLI_VERSION} already installed. Skipping."
      return
    fi
    info "signal-cli ${EXISTING} installed, upgrading to ${SIGNAL_CLI_VERSION}..."
  else
    info "Installing signal-cli ${SIGNAL_CLI_VERSION}..."
  fi

  SCTMPDIR=$(mktemp -d)
  trap 'rm -rf "$SCTMPDIR"' EXIT

  TARBALL="signal-cli-${SIGNAL_CLI_VERSION}.tar.gz"
  URL="https://github.com/AsamK/signal-cli/releases/download/v${SIGNAL_CLI_VERSION}/${TARBALL}"

  wget -q --show-progress -O "${SCTMPDIR}/${TARBALL}" "${URL}" || \
    curl -fsSL -o "${SCTMPDIR}/${TARBALL}" "${URL}"

  if $ROOT_INSTALL; then
    $SUDO tar -xzf "${SCTMPDIR}/${TARBALL}" -C /opt/
    $SUDO ln -sf "/opt/signal-cli-${SIGNAL_CLI_VERSION}/bin/signal-cli" /usr/local/bin/signal-cli
  else
    mkdir -p "${HOME}/.local/opt"
    tar -xzf "${SCTMPDIR}/${TARBALL}" -C "${HOME}/.local/opt/"
    mkdir -p "${HOME}/.local/bin"
    ln -sf "${HOME}/.local/opt/signal-cli-${SIGNAL_CLI_VERSION}/bin/signal-cli" "${HOME}/.local/bin/signal-cli"
  fi

  success "signal-cli ${SIGNAL_CLI_VERSION} installed."
}

# Install Go (if needed and user consents)
install_go() {
  info "Go is required to build datawatch from source."
  info "Installing Go via the official installer..."

  local GO_VERSION="1.22.4"
  local ARCH; ARCH=$(uname -m)
  local GOARCH
  case $ARCH in
    x86_64)  GOARCH="amd64" ;;
    aarch64) GOARCH="arm64" ;;
    armv7l)  GOARCH="armv6l" ;;
    *) error "Unsupported arch for Go install: ${ARCH}" ;;
  esac

  local GOTARBALL="go${GO_VERSION}.linux-${GOARCH}.tar.gz"
  local GOURL="https://go.dev/dl/${GOTARBALL}"
  local TMPGO; TMPGO=$(mktemp -d)

  wget -q --show-progress -O "${TMPGO}/${GOTARBALL}" "${GOURL}" || \
    curl -fsSL -o "${TMPGO}/${GOTARBALL}" "${GOURL}"

  if $ROOT_INSTALL; then
    $SUDO rm -rf /usr/local/go
    $SUDO tar -C /usr/local -xzf "${TMPGO}/${GOTARBALL}"
    export PATH="/usr/local/go/bin:${PATH}"
    success "Go ${GO_VERSION} installed to /usr/local/go."
  else
    local GOVERSIONED="${HOME}/.local/go-${GO_VERSION}"
    local GOSYMLINK="${HOME}/.local/go"
    mkdir -p "${HOME}/.local"
    # Remove existing versioned dir if present, then extract fresh
    rm -rf "${GOVERSIONED}"
    local GOEXTRACT; GOEXTRACT=$(mktemp -d)
    tar -C "${GOEXTRACT}" -xzf "${TMPGO}/${GOTARBALL}"
    # The tarball always extracts to a subdirectory named 'go'
    mv "${GOEXTRACT}/go" "${GOVERSIONED}"
    rm -rf "${GOEXTRACT}"
    # Update the symlink (remove stale symlink or old plain dir first)
    rm -rf "${GOSYMLINK}"
    ln -sfn "${GOVERSIONED}" "${GOSYMLINK}"
    export PATH="${GOSYMLINK}/bin:${PATH}"
    success "Go ${GO_VERSION} installed to ${GOSYMLINK} -> ${GOVERSIONED}."
    warn "Add to your shell profile: export PATH=\"\${HOME}/.local/go/bin:\${PATH}\""
  fi

  rm -rf "${TMPGO}"
}

# Install datawatch binary
install_binary() {
  info "Installing datawatch binary..."

  if $ROOT_INSTALL; then
    INSTALL_DIR="/usr/local/bin"
  else
    INSTALL_DIR="${HOME}/.local/bin"
    mkdir -p "${INSTALL_DIR}"
  fi

  # Detect architecture for prebuilt binary selection
  local ARCH; ARCH=$(uname -m)
  local GOARCH
  case $ARCH in
    x86_64)  GOARCH="amd64" ;;
    aarch64) GOARCH="arm64" ;;
    armv7l)  GOARCH="arm"   ;;
    *)
      warn "Unknown arch: ${ARCH}. Prebuilt binary not available — will build from source."
      GOARCH=""
      ;;
  esac

  # 1. Try prebuilt binary first (GoReleaser archive format)
  if [[ -n "${GOARCH}" ]]; then
    local ARCHIVE_NAME="${BINARY_NAME}_${VERSION}_linux_${GOARCH}.tar.gz"
    local ARCHIVE_URL="https://github.com/${REPO}/releases/download/v${VERSION}/${ARCHIVE_NAME}"
    local TMPARCHIVE; TMPARCHIVE=$(mktemp -d)
    info "Downloading prebuilt binary from ${ARCHIVE_URL} ..."
    if wget -q --show-progress -O "${TMPARCHIVE}/${ARCHIVE_NAME}" "${ARCHIVE_URL}" 2>/dev/null || \
       curl -fsSL -o "${TMPARCHIVE}/${ARCHIVE_NAME}" "${ARCHIVE_URL}" 2>/dev/null; then
      # Extract the binary (it sits at the top level of the archive)
      if tar -xzf "${TMPARCHIVE}/${ARCHIVE_NAME}" -C "${TMPARCHIVE}" "${BINARY_NAME}" 2>/dev/null || \
         tar -xzf "${TMPARCHIVE}/${ARCHIVE_NAME}" -C "${TMPARCHIVE}" 2>/dev/null; then
        if [[ -f "${TMPARCHIVE}/${BINARY_NAME}" ]]; then
          install -m 755 "${TMPARCHIVE}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
          rm -rf "${TMPARCHIVE}"
          success "Binary v${VERSION} installed to ${INSTALL_DIR}/${BINARY_NAME}."
          return
        fi
      fi
    fi
    rm -rf "${TMPARCHIVE}"
    warn "Prebuilt binary not available for v${VERSION} (${GOARCH}). Falling back to build from source."
  fi

  # 2. Build from source.
  #    Detect if we're running from inside the source repo (developer workflow).
  local SCRIPT_DIR; SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
  local REPO_ROOT; REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
  local LOCAL_SOURCE=false
  if [[ -f "${REPO_ROOT}/go.mod" && -f "${REPO_ROOT}/cmd/datawatch/main.go" ]]; then
    LOCAL_SOURCE=true
  fi

  # Ensure Go is available; install it if not.
  if ! command -v go &>/dev/null; then
    install_go
  fi

  if $LOCAL_SOURCE; then
    info "Building from local source (${REPO_ROOT})..."
    go build -C "${REPO_ROOT}" -ldflags="-X main.Version=${VERSION}" \
      -o "${INSTALL_DIR}/${BINARY_NAME}" ./cmd/datawatch/
  elif command -v git &>/dev/null; then
    info "Cloning source and building..."
    local TMPBUILD; TMPBUILD=$(mktemp -d)
    git clone --depth 1 "https://github.com/${REPO}.git" "${TMPBUILD}"
    go build -C "${TMPBUILD}" -ldflags="-X main.Version=${VERSION}" \
      -o "${INSTALL_DIR}/${BINARY_NAME}" ./cmd/datawatch/
    rm -rf "${TMPBUILD}"
  else
    error "Cannot install datawatch: no prebuilt binary for v${VERSION}, git is not available, and Go is not installed.
  Options:
    1. Install git or Go (https://go.dev/dl/) and re-run this installer.
    2. Download a binary directly from https://github.com/${REPO}/releases"
  fi

  success "Built and installed from source to ${INSTALL_DIR}/${BINARY_NAME}."
}

# Create directories
create_dirs() {
  info "Creating data directories..."
  if $ROOT_INSTALL; then
    $SUDO mkdir -p /etc/datawatch /var/lib/datawatch /var/log/datawatch
    # Create system user if it doesn't exist
    if ! id datawatch &>/dev/null; then
      $SUDO useradd --system --no-create-home --shell /usr/sbin/nologin \
        --home-dir /var/lib/datawatch datawatch
    fi
    $SUDO chown -R datawatch:datawatch /var/lib/datawatch /var/log/datawatch /etc/datawatch
  else
    mkdir -p "${HOME}/.datawatch" "${HOME}/.local/share/signal-cli"
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
    $SUDO cp "$(dirname "$0")/systemd/datawatch.service" /etc/systemd/system/
    $SUDO systemctl daemon-reload
    $SUDO systemctl enable datawatch
    success "System service installed. Start with: sudo systemctl start datawatch"
  else
    info "Installing user systemd service..."
    SERVICE_DIR="${HOME}/.config/systemd/user"
    mkdir -p "${SERVICE_DIR}"

    # Write user service file
    cat > "${SERVICE_DIR}/datawatch.service" <<EOF
[Unit]
Description=datawatch - Multi-backend AI coding session daemon
After=network-online.target default.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${HOME}/.local/bin/datawatch start
ExecReload=/bin/kill -HUP \$MAINPID
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=datawatch
Environment=HOME=${HOME}
Environment=PATH=${HOME}/.local/bin:/usr/local/bin:/usr/bin:/bin

[Install]
WantedBy=default.target
EOF

    systemctl --user daemon-reload
    systemctl --user enable datawatch
    success "User service installed. Start with: systemctl --user start datawatch"
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
  echo "Quick start (Signal backend):"
  echo ""
  echo "  1. Link your device — creates the control group automatically:"
  echo "       datawatch link"
  echo "     Enter your Signal number, scan the QR code in Signal"
  echo "     (Settings → Linked Devices → Link New Device)."
  echo "     datawatch creates 'datawatch-\$(hostname)' group and saves config."
  echo ""
  if $INSTALL_SERVICE; then
    if $ROOT_INSTALL; then
      echo "  2. Start the service:"
      echo "       sudo systemctl start datawatch"
    else
      echo "  2. Start the service:"
      echo "       systemctl --user start datawatch"
    fi
  else
    echo "  2. Start the daemon:"
    echo "       datawatch start"
  fi
  echo ""
  echo "  3. Send 'help' in the 'datawatch-\$(hostname)' Signal group to verify."
  echo ""
  echo "For Telegram, Matrix, Discord, or other backends see:"
  echo "  https://github.com/${REPO}/blob/main/docs/messaging-backends.md"
  echo ""
  echo "Full documentation: https://github.com/${REPO}"
}

# Main
main() {
  info "datawatch installer v${VERSION}"
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
