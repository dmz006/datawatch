#!/usr/bin/env bash
# datawatch-stats installer — Shape B standalone observer peer
#
# Installs the datawatch-stats reporter on a host that does NOT run the
# full datawatch daemon. Once installed, the reporter pushes system
# snapshots (CPU/mem/disk/GPU/processes) to a parent datawatch instance.
#
# Usage:
#   bash install-stats.sh --datawatch https://parent:8443 [OPTIONS]
#
# Options:
#   --datawatch URL       Parent datawatch URL (required, or DATAWATCH_URL env)
#   --name NAME           Peer name reported to parent (default: hostname)
#   --version X.Y.Z       Pin a specific release (default: latest)
#   --insecure-tls        Skip TLS verify (self-signed parent cert)
#   --push-interval DUR   Snapshot cadence (default: 5s)
#   --service             Install and enable as a systemd user service
#   --root                Install system-wide (/usr/local/bin + system service)
#   --help                Show this help
#
# The bearer token minted by the parent is stored in
# ~/.datawatch-stats/peer.token and is never written to stdout/logs.
set -euo pipefail

REPO="dmz006/datawatch"
BINARY="datawatch-stats"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info()    { echo -e "${BLUE}[INFO]${NC} $*"; }
success() { echo -e "${GREEN}[OK]${NC} $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
error()   { echo -e "${RED}[ERROR]${NC} $*" >&2; exit 1; }

PARENT_URL="${DATAWATCH_URL:-}"
PEER_NAME="${HOSTNAME:-$(hostname)}"
PINNED_VERSION=""
INSECURE_TLS=false
PUSH_INTERVAL="5s"
INSTALL_SERVICE=false
ROOT_INSTALL=false
HELP=false

_args=("$@")
_i=0
while [[ $_i -lt ${#_args[@]} ]]; do
  _arg="${_args[$_i]}"
  case "$_arg" in
    --datawatch=*) PARENT_URL="${_arg#--datawatch=}" ;;
    --datawatch)   _i=$((_i+1)); PARENT_URL="${_args[$_i]:-}" ;;
    --name=*)      PEER_NAME="${_arg#--name=}" ;;
    --name)        _i=$((_i+1)); PEER_NAME="${_args[$_i]:-}" ;;
    --version=*)   PINNED_VERSION="${_arg#--version=}" ;;
    --version)     _i=$((_i+1)); PINNED_VERSION="${_args[$_i]:-}" ;;
    --push-interval=*) PUSH_INTERVAL="${_arg#--push-interval=}" ;;
    --push-interval)   _i=$((_i+1)); PUSH_INTERVAL="${_args[$_i]:-}" ;;
    --insecure-tls)    INSECURE_TLS=true ;;
    --service)    INSTALL_SERVICE=true ;;
    --root)       ROOT_INSTALL=true ;;
    --help|-h)    HELP=true ;;
  esac
  _i=$((_i+1))
done
unset _args _i _arg

if $HELP; then
  cat <<'EOF'
datawatch-stats installer — Shape B observer peer

Usage: bash install-stats.sh --datawatch https://parent:8443 [OPTIONS]

Options:
  --datawatch URL       Parent datawatch base URL (required; or DATAWATCH_URL env)
  --name NAME           Stable peer name sent to parent (default: hostname)
  --version X.Y.Z       Pin release version (default: fetch latest)
  --insecure-tls        Skip TLS cert verify for self-signed parent certs
  --push-interval DUR   Snapshot cadence (default: 5s, min 1s)
  --service             Install and enable as systemd service
  --root                System-wide install (requires sudo; default: user install)
  --help                This text

Non-root (default):
  Binary:   ~/.local/bin/datawatch-stats
  Token:    ~/.datawatch-stats/peer.token
  Service:  ~/.config/systemd/user/datawatch-stats.service

Root (--root):
  Binary:   /usr/local/bin/datawatch-stats
  Token:    /var/lib/datawatch-stats/peer.token
  Service:  /etc/systemd/system/datawatch-stats.service
EOF
  exit 0
fi

# Detect arch
ARCH=$(uname -m)
case $ARCH in
  x86_64)  GOARCH="amd64" ;;
  aarch64) GOARCH="arm64" ;;
  *) error "Unsupported architecture: ${ARCH}. Build from source: go build ./cmd/datawatch-stats/" ;;
esac

# Fetch latest version from GitHub if not pinned
fetch_latest_version() {
  local ver=""
  if command -v curl &>/dev/null; then
    ver=$(curl -fsSL --max-time 10 -o /dev/null -w "%{url_effective}" \
          "https://github.com/${REPO}/releases/latest" 2>/dev/null \
          | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
  fi
  if [[ ! "${ver}" =~ ^[0-9]+\.[0-9]+ ]] && command -v wget &>/dev/null; then
    ver=$(wget -qO- --timeout=10 --server-response \
          "https://github.com/${REPO}/releases/latest" 2>&1 \
          | grep -i "Location:" | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | tail -1)
  fi
  if [[ ! "${ver}" =~ ^[0-9]+\.[0-9]+ ]]; then
    error "Could not determine latest version. Use --version X.Y.Z to pin one."
  fi
  echo "${ver}"
}

VERSION="${PINNED_VERSION:-$(fetch_latest_version)}"
info "Installing datawatch-stats v${VERSION} (${GOARCH})"

# Install directories
if $ROOT_INSTALL; then
  INSTALL_DIR="/usr/local/bin"
  TOKEN_DIR="/var/lib/datawatch-stats"
  SUDO="sudo"
  if [[ $EUID -ne 0 ]]; then
    sudo -v || error "sudo authentication failed"
  fi
else
  INSTALL_DIR="${HOME}/.local/bin"
  TOKEN_DIR="${HOME}/.datawatch-stats"
  SUDO=""
  mkdir -p "${INSTALL_DIR}"
fi

mkdir -p "${TOKEN_DIR}"
chmod 700 "${TOKEN_DIR}"

# Download binary from GitHub release
ARCHIVE="datawatch_${VERSION}_linux_${GOARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/v${VERSION}/${ARCHIVE}"
TMPDIR=$(mktemp -d)
trap 'rm -rf "${TMPDIR}"' EXIT

info "Downloading ${URL} ..."
if command -v curl &>/dev/null; then
  curl -fsSL --max-time 120 -o "${TMPDIR}/${ARCHIVE}" "${URL}" \
    || error "Download failed. Check version v${VERSION} exists at https://github.com/${REPO}/releases"
elif command -v wget &>/dev/null; then
  wget -q --show-progress --timeout=120 -O "${TMPDIR}/${ARCHIVE}" "${URL}" \
    || error "Download failed."
else
  error "Neither curl nor wget found. Install one and retry."
fi

tar -xzf "${TMPDIR}/${ARCHIVE}" -C "${TMPDIR}"
FOUND=$(find "${TMPDIR}" -maxdepth 3 -name "${BINARY}" -not -name "*.tar.gz" | head -1)
if [[ -z "${FOUND}" || ! -f "${FOUND}" ]]; then
  error "${BINARY} not found in archive ${ARCHIVE}. This release may predate goreleaser support for datawatch-stats (added v8.25.2+). Either upgrade to a newer release or build from source: GOOS=linux GOARCH=${GOARCH} go build -o ${BINARY} ./cmd/datawatch-stats/"
fi

${SUDO} install -m 755 "${FOUND}" "${INSTALL_DIR}/${BINARY}"
success "Binary installed to ${INSTALL_DIR}/${BINARY}"

# Verify it runs
"${INSTALL_DIR}/${BINARY}" --version 2>/dev/null | head -1 || true

# Service setup
if $INSTALL_SERVICE; then
  if ! command -v systemctl &>/dev/null; then
    warn "systemd not found — skipping service install. Run manually:"
    _insecure_flag=""
    $INSECURE_TLS && _insecure_flag=" --insecure-tls"
    echo "  ${INSTALL_DIR}/${BINARY} --datawatch ${PARENT_URL} --name ${PEER_NAME} --push-interval ${PUSH_INTERVAL}${_insecure_flag} --token-file ${TOKEN_DIR}/peer.token"
  else
    INSECURE_FLAG=""
    $INSECURE_TLS && INSECURE_FLAG=" --insecure-tls"

    if $ROOT_INSTALL; then
      SERVICE_PATH="/etc/systemd/system/datawatch-stats.service"
      ${SUDO} tee "${SERVICE_PATH}" > /dev/null <<EOF
[Unit]
Description=datawatch-stats — Shape B observer peer
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/${BINARY} --datawatch ${PARENT_URL} --name ${PEER_NAME} --push-interval ${PUSH_INTERVAL}${INSECURE_FLAG} --token-file ${TOKEN_DIR}/peer.token
Restart=on-failure
RestartSec=15
StandardOutput=journal
StandardError=journal
SyslogIdentifier=datawatch-stats

[Install]
WantedBy=multi-user.target
EOF
      ${SUDO} systemctl daemon-reload
      ${SUDO} systemctl enable datawatch-stats
      success "System service installed. Start: sudo systemctl start datawatch-stats"
    else
      SERVICE_DIR="${HOME}/.config/systemd/user"
      mkdir -p "${SERVICE_DIR}"
      cat > "${SERVICE_DIR}/datawatch-stats.service" <<EOF
[Unit]
Description=datawatch-stats — Shape B observer peer
After=network-online.target default.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/${BINARY} --datawatch ${PARENT_URL} --name ${PEER_NAME} --push-interval ${PUSH_INTERVAL}${INSECURE_FLAG} --token-file ${TOKEN_DIR}/peer.token
Restart=on-failure
RestartSec=15
StandardOutput=journal
StandardError=journal
SyslogIdentifier=datawatch-stats
Environment=HOME=${HOME}
Environment=PATH=${INSTALL_DIR}:/usr/local/bin:/usr/bin:/bin

[Install]
WantedBy=default.target
EOF
      systemctl --user daemon-reload
      systemctl --user enable datawatch-stats
      success "User service installed. Start: systemctl --user start datawatch-stats"
      info "Enable lingering for boot-time start: loginctl enable-linger ${USER}"
    fi
  fi
fi

echo ""
success "datawatch-stats v${VERSION} ready."
echo ""
echo "  Token storage:  ${TOKEN_DIR}/peer.token"
echo "  Register peer:  On the parent, run: datawatch observer peer register ${PEER_NAME} B"
echo ""
if ! $INSTALL_SERVICE; then
  _insecure_flag=""
  $INSECURE_TLS && _insecure_flag=" --insecure-tls"
  echo "  Start manually:"
  echo "    ${INSTALL_DIR}/${BINARY} \\"
  echo "      --datawatch ${PARENT_URL:-<parent-url>} \\"
  echo "      --name ${PEER_NAME} \\"
  echo "      --push-interval ${PUSH_INTERVAL} \\"
  echo "      --token-file ${TOKEN_DIR}/peer.token${_insecure_flag}"
fi
