#!/usr/bin/env bash
# ci-install-helm.sh — install a pinned Helm release in CI.
#
# Usage: ci-install-helm.sh <helm-version-tag> <install-dir>
#
# The Helm release archive is fetched from get.helm.sh for the exact tag,
# saved to a temporary file, and only then unpacked. Nothing is piped into a
# shell, so a failed or truncated fetch stops the calling CI step instead of
# running partial content. Only the linux-amd64/helm binary is installed.
set -euo pipefail

usage() {
  echo "usage: $0 <helm-version-tag> <install-dir>" >&2
}

# Temp paths to remove on exit (the archive and the extraction directory).
TMP_PATHS=()

cleanup() {
  if (( ${#TMP_PATHS[@]} > 0 )); then
    rm -rf -- "${TMP_PATHS[@]}"
  fi
}

# download_helm_archive <url> <dest-file>
# Returns curl's exit status on failure, after printing the status and URL.
download_helm_archive() {
  local url="$1" dest="$2" rc=0
  curl --fail --show-error --silent --location \
    --retry 5 --retry-delay 10 --retry-all-errors \
    --connect-timeout 20 --max-time 120 \
    -o "$dest" "$url" || rc=$?
  if (( rc != 0 )); then
    echo "error: failed to download helm archive (curl exit status $rc): $url" >&2
    return "$rc"
  fi
}

# install_helm <archive> <install-dir>
# Extracts linux-amd64/helm from the archive and installs it into install-dir.
install_helm() {
  local archive="$1" install_dir="$2" extract_dir
  extract_dir="$(mktemp -d)"
  TMP_PATHS+=("$extract_dir")

  tar -xzf "$archive" -C "$extract_dir" linux-amd64/helm

  mkdir -p -- "$install_dir"
  if command -v install >/dev/null 2>&1; then
    install -m 0755 "$extract_dir/linux-amd64/helm" "$install_dir/helm"
  else
    cp "$extract_dir/linux-amd64/helm" "$install_dir/helm"
    chmod 0755 "$install_dir/helm"
  fi
}

main() {
  if (( $# != 2 )) || [[ -z "$1" || -z "$2" ]]; then
    usage
    exit 2
  fi

  local tag="$1" install_dir="$2"
  local url="https://get.helm.sh/helm-${tag}-linux-amd64.tar.gz"
  local tmp rc=0

  tmp="$(mktemp)"
  TMP_PATHS+=("$tmp")
  trap cleanup EXIT

  download_helm_archive "$url" "$tmp" || rc=$?
  if (( rc != 0 )); then
    exit "$rc"
  fi

  install_helm "$tmp" "$install_dir"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
