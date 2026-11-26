#!/usr/bin/env bash
# check-test-isolation.sh — release gate: fail the test run when a test reads or
# writes outside its own temporary area and the repository.
#
# Usage:  scripts/check-test-isolation.sh [go test arguments]   (default ./...)
#
# Runs `go test -count=1 -exec testisolation-guard`: every test binary gets its
# own sandbox (private /tmp, private home, no network) holding planted decoys,
# so a test that leaks cannot reach real files while it is being caught.
# Needs Linux and bubblewrap (bwrap); it fails rather than run unguarded.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if ! command -v bwrap >/dev/null 2>&1; then
  echo "test-isolation: bwrap (bubblewrap) is not installed" >&2
  exit 1
fi

bindir="$(mktemp -d)"
trap 'rm -rf "$bindir"' EXIT INT TERM
go build -o "$bindir/testisolation-guard" ./cmd/testisolation-guard

args=("$@")
[ ${#args[@]} -gt 0 ] || args=(./...)
env -u TMUX -u TMUX_TMPDIR go test -count=1 -exec "$bindir/testisolation-guard" "${args[@]}"
echo "test-isolation: PASS"
