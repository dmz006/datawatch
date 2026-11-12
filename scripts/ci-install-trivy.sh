#!/usr/bin/env bash
# ci-install-trivy.sh — install a pinned Trivy release in CI.
#
# Usage: ci-install-trivy.sh <trivy-version-tag> <install-dir>
#
# The Trivy install script is fetched from the tag itself (never main), saved
# to a temporary file, and only then executed with sh. The download is never
# piped straight into a shell, so a failed or truncated fetch stops the calling
# CI step instead of running partial content.
set -euo pipefail

usage() {
  echo "usage: $0 <trivy-version-tag> <install-dir>" >&2
}

# download_install_script <url> <dest-file>
# Returns curl's exit status on failure, after printing the status and URL.
download_install_script() {
  local url="$1" dest="$2" rc=0
  curl --fail --show-error --silent --location \
    --retry 5 --retry-delay 10 --retry-all-errors \
    --connect-timeout 20 --max-time 120 \
    -o "$dest" "$url" || rc=$?
  if (( rc != 0 )); then
    echo "error: failed to download Trivy install script (curl exit status $rc): $url" >&2
    return "$rc"
  fi
}

# install_trivy <script-file> <install-dir> <version-tag>
install_trivy() {
  local script="$1" install_dir="$2" tag="$3"
  sh -s -- -b "$install_dir" "$tag" < "$script"
}

main() {
  if (( $# != 2 )) || [[ -z "$1" || -z "$2" ]]; then
    usage
    exit 2
  fi

  local tag="$1" install_dir="$2"
  local url="https://raw.githubusercontent.com/aquasecurity/trivy/${tag}/contrib/install.sh"
  local tmp rc=0

  tmp="$(mktemp)"
  # The trap runs after main's locals are gone, so bake the path in now.
  # shellcheck disable=SC2064 # intentional: expand $tmp at trap-set time
  trap "rm -f -- $(printf '%q' "$tmp")" EXIT

  download_install_script "$url" "$tmp" || rc=$?
  if (( rc != 0 )); then
    exit "$rc"
  fi

  install_trivy "$tmp" "$install_dir" "$tag"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
