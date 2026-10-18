#!/usr/bin/env bash
# check-clean-tree.sh — CI-friendly guard for test runs that must not drop
# files into the repository tree.
#
# Usage:
#   bash scripts/check-clean-tree.sh -- <command> [args...]
# e.g.
#   bash scripts/check-clean-tree.sh -- go test ./...
#
# Behavior:
#   * cds to the repository top level (resolved via git on the PATH),
#   * snapshots 'git status --porcelain --ignored' before and after the
#     wrapped command,
#   * streams the wrapped command's output and remembers its exit code,
#   * if any new untracked/ignored entries appeared, prints each new path
#     to stderr and exits non-zero (even if the wrapped command succeeded),
#   * otherwise exits with the wrapped command's own exit code.
#
# This guard only reports; it never deletes anything.
set -u -o pipefail

die() {
  printf 'check-clean-tree: %s\n' "$*" >&2
  exit "${2:-127}"
}

# --- Argument parsing: require an explicit '--' separator first -------------
if [[ "${1:-}" != "--" ]]; then
  printf 'usage: %s -- <command> [args...]\n' "$0" >&2
  exit 2
fi
shift
if [[ $# -lt 1 ]]; then
  printf 'usage: %s -- <command> [args...]\n' "$0" >&2
  exit 2
fi

# --- Resolve git at runtime -------------------------------------------------
if ! command -v git >/dev/null 2>&1; then
  printf 'check-clean-tree: error: git was not found on PATH; install git or add it to PATH\n' >&2
  exit 127
fi

toplevel="$(git rev-parse --show-toplevel)" || die "could not determine the repository top level (are you inside a git work tree?)"
cd -- "$toplevel" || die "could not cd to repository top level: $toplevel"

# --- Snapshot helper (full porcelain lines, safe for paths with spaces) -----
snapshot_tree() {
  git status --porcelain --ignored
}

baseline="$(snapshot_tree)" || die "failed to capture baseline 'git status --porcelain --ignored'"

# --- Run the wrapped command, streaming its output --------------------------
# No 'set -e' applies here: a failing wrapped command must not abort the
# script before the post-run tree comparison happens.
"$@"
wrapped_exit=$?

after="$(snapshot_tree)" || {
  printf 'check-clean-tree: error: failed to capture post-run tree state; treating as failure\n' >&2
  if (( wrapped_exit != 0 )); then
    exit "$wrapped_exit"
  fi
  exit 1
}

# --- Diff the two snapshots: any line present after but absent before -------
new_entries=0
while IFS= read -r tree_line; do
  [[ -n "$tree_line" ]] || continue
  if ! grep -Fxq -- "$tree_line" <<< "$baseline"; then
    # Porcelain format: two status chars, a space, then the path.
    new_path="${tree_line:3}"
    printf 'check-clean-tree: new tree entry: %s\n' "$new_path" >&2
    new_entries=$(( new_entries + 1 ))
  fi
done <<< "$after"

if (( new_entries > 0 )); then
  printf 'check-clean-tree: FAIL: %d new file(s) dropped into the repository tree\n' "$new_entries" >&2
  if (( wrapped_exit != 0 )); then
    exit "$wrapped_exit"
  fi
  exit 1
fi

exit "$wrapped_exit"
