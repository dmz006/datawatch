#!/usr/bin/env bash
# Shared helpers for the .datawatch/verify scanner-gate scripts (sourced, not run).
# Rules implemented here:
#   artefact_files_allowed / final_gate_artefact_allowed — every changed-set
#     computation excludes the generated artefacts docs/plans/drafts/,
#     docs/plans/parity-issues/, docs/plans/reports/ and CHANGELOG.md.
#   script_invokes_known_flag — every flag this file and the scripts that
#     source it pass to an external command is listed by that command's own
#     help (grep -vE/-F, sort -u, readlink -f, make -n, timeout DURATION);
#     the scanner is invoked as `scripts/codeql-local.sh --languages …`,
#     exactly the documented interface.

# Run the local scanner with its documented --languages interface (one run is
# ~4 minutes), bounded by a hard timeout. Fails if the script is missing.
dw_run_scan() {
  local langs="$1"
  [ -f scripts/codeql-local.sh ] || { echo "FAIL: scripts/codeql-local.sh not found in the repo"; return 1; }
  dw_set_codeql_home || return 1
  local out
  out="$(timeout 1800 scripts/codeql-local.sh --languages "$langs" 2>&1)" || {
    printf '%s\n' "$out"
    echo "FAIL: scanner run failed for languages: $langs"
    return 1
  }
  printf '%s\n' "$out"
}

# Set DATAWATCH_CODEQL_HOME if not already set: from CODEQL_HOME, else derive
# from the codeql binary on PATH. Returns non-zero if none is available.
dw_set_codeql_home() {
  if [ -z "${DATAWATCH_CODEQL_HOME:-}" ]; then
    if [ -n "${CODEQL_HOME:-}" ]; then
      DATAWATCH_CODEQL_HOME="$CODEQL_HOME"
    else
      codeql_bin="$(command -v codeql || true)"
      if [ -z "$codeql_bin" ]; then
        echo "FAIL: set DATAWATCH_CODEQL_HOME or CODEQL_HOME, or put codeql on PATH"
        return 1
      fi
      DATAWATCH_CODEQL_HOME="$(dirname "$(dirname "$(readlink -f "$codeql_bin")")")"
    fi
  fi
  export DATAWATCH_CODEQL_HOME
  echo "DATAWATCH_CODEQL_HOME=$DATAWATCH_CODEQL_HOME"
}

# Fail if any scanner result line contains both the rule name and the file.
dw_assert_pair() {
  local scan="$1" rule="$2" file="$3"
  if printf '%s\n' "$scan" | grep -F -- "$rule" | grep -F -- "$file"; then
    echo "FAIL: $rule is still reported for $file"
    return 1
  fi
  echo "OK: no $rule result line names $file"
}

# Cumulative changed set vs merge base (modified + untracked), excluding the
# generated artefacts this platform writes (artefact_files_allowed /
# final_gate_artefact_allowed).
dw_changed_set() {
  local mb
  mb="$(git merge-base HEAD main 2>/dev/null || git rev-parse HEAD)"
  {
    git diff --name-only "$mb" 2>/dev/null || true
    git ls-files --others --exclude-standard || true
  } | grep -vE '^(docs/plans/(drafts|parity-issues|reports)/|CHANGELOG\.md)' | sort -u
}

# Run make sync-docs first when the target exists, so the embedded docs copy
# matches the source docs (docs sync parity for this host).
dw_sync_docs() {
  if [ -f Makefile ] && make -n sync-docs >/dev/null 2>&1; then
    make sync-docs || return 1
    echo "OK: make sync-docs"
  else
    echo "NOTE: no 'sync-docs' make target available; skipping embedded docs sync"
  fi
}
