#!/usr/bin/env bash
# Shared helpers for the decompose-abort verification scripts.
cd "$(git rev-parse --show-toplevel)" || exit 1
unset TMUX TMUX_TMPDIR
rc=0
fail(){ echo "FAIL: $*"; rc=1; }
# Files changed since the merge base plus untracked files, minus the
# daemon-generated artefacts. Informational: never asserted against a list.
changed_files(){
  base=$(git merge-base HEAD main 2>/dev/null || echo HEAD)
  { git diff --name-only "$base"; git ls-files --others --exclude-standard; } | sort -u \
    | grep -v -e '^docs/plans/drafts/' -e '^docs/plans/parity-issues/' -e '^docs/plans/reports/' -e '^CHANGELOG.md$'
}
# gofmt only the named Go files that exist; never a directory.
gofmt_named(){ for f in "$@"; do [ -f "$f" ] || continue; [ -z "$(gofmt -l "$f")" ] || fail "gofmt: $f"; done; }
need_file(){ [ -s "$1" ] || fail "missing or empty: $1"; }
