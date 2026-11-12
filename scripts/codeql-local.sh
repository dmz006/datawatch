#!/usr/bin/env bash
# Local, non-uploading CodeQL run that mirrors GitHub default setup.
#
# Runs the default code-scanning suite per language with the CodeQL bundle
# version default setup uses, writes SARIF under the CodeQL home and prints
# a report. With --compare the results are matched against the open and
# dismissed code-scanning alerts GitHub holds for refs/heads/main (read-only
# API calls). Nothing is ever uploaded or posted.
#
# See docs/plans/2026-10-30-codeql-default-setup-and-local-scan.md.
set -euo pipefail

# Pinned CodeQL bundle version: the one default setup last used (re-pin after
# `--check-version` reports drift).
CODEQL_VERSION="2.27.3"

DEFAULT_LANGUAGES="actions go javascript-typescript python"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

CODEQL_HOME="${DATAWATCH_CODEQL_HOME:-.datawatch/codeql}"

usage() {
  cat <<EOF
Usage: scripts/codeql-local.sh [options]

Run the CodeQL default code-scanning suite locally, without uploading anything.

Options:
  --help             Show this help and exit.
  --install          Download and extract CodeQL bundle ${CODEQL_VERSION} into the CodeQL home if missing.
  --languages "L.."  Space-separated languages (default: ${DEFAULT_LANGUAGES}).
                     Allowed: actions go javascript-typescript python.
  --compare          Fetch open and dismissed alerts for refs/heads/main with gh and mark
                     each local result open on GitHub, dismissed on GitHub or not on GitHub.
                     Without a usable gh the run continues without the comparison.
  --check-version    Print the pinned bundle version and the version default setup last used
                     (code-scanning analyses API); exit 1 on drift or if it cannot be read.

CodeQL home: \${DATAWATCH_CODEQL_HOME:-.datawatch/codeql} (gitignored).
Bundle: <home>/bundle/codeql/codeql, else 'codeql' on PATH.
Previous SARIF files in <home>/out are removed at the start of a run.

Exit codes: 0 clean, 1 high or critical findings not dismissed on GitHub (or any
high/critical when the comparison was not usable), 2 usage, 3 CodeQL CLI missing,
4 a CodeQL step failed.
EOF
}

die_usage() {
  echo "codeql-local: $1" >&2
  echo "Try: scripts/codeql-local.sh --help" >&2
  exit 2
}

do_install=0
do_compare=0
do_check_version=0
languages="$DEFAULT_LANGUAGES"

while [ $# -gt 0 ]; do
  case "$1" in
    --help|-h) usage; exit 0 ;;
    --install) do_install=1 ;;
    --compare) do_compare=1 ;;
    --check-version) do_check_version=1 ;;
    --languages)
      [ $# -ge 2 ] || die_usage "--languages needs a value"
      languages="$2"; shift ;;
    --languages=*) languages="${1#--languages=}" ;;
    *) die_usage "unknown option: $1" ;;
  esac
  shift
done

# --- helpers -----------------------------------------------------------------

gh_usable() {
  command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1
}

default_setup_version() {
  gh api 'repos/{owner}/{repo}/code-scanning/analyses?ref=refs/heads/main&per_page=1' \
    --jq '.[0].tool.version' 2>/dev/null
}

if [ "$do_check_version" -eq 1 ]; then
  echo "pinned bundle:       ${CODEQL_VERSION}"
  if ! gh_usable; then
    echo "default setup used:  unknown (gh missing or not authenticated)"
    exit 1
  fi
  used="$(default_setup_version || true)"
  if [ -z "$used" ] || [ "$used" = "null" ]; then
    echo "default setup used:  unknown (no analysis found for refs/heads/main)"
    exit 1
  fi
  echo "default setup used:  ${used}"
  if [ "$used" != "$CODEQL_VERSION" ]; then
    echo "DRIFT: re-pin CODEQL_VERSION in scripts/codeql-local.sh before trusting a comparison." >&2
    exit 1
  fi
  exit 0
fi

lang_pack() {
  case "$1" in
    actions) echo "codeql/actions-queries" ;;
    go) echo "codeql/go-queries" ;;
    javascript-typescript) echo "codeql/javascript-queries" ;;
    python) echo "codeql/python-queries" ;;
    *) return 1 ;;
  esac
}

lang_suite() {
  case "$1" in
    actions) echo "actions-code-scanning.qls" ;;
    go) echo "go-code-scanning.qls" ;;
    javascript-typescript) echo "javascript-code-scanning.qls" ;;
    python) echo "python-code-scanning.qls" ;;
  esac
}

for lang in $languages; do
  lang_pack "$lang" >/dev/null || die_usage "unsupported language: $lang"
done
[ -n "${languages// /}" ] || die_usage "--languages is empty"

bundle_codeql="$CODEQL_HOME/bundle/codeql/codeql"

install_bundle() {
  local platform archive
  case "$(uname -s)" in
    Linux) platform="linux64" ;;
    Darwin) platform="osx64" ;;
    *) echo "codeql-local: no bundle for this platform" >&2; exit 3 ;;
  esac
  archive="codeql-bundle-${platform}.tar.gz"
  if ! gh_usable; then
    echo "codeql-local: --install needs an authenticated gh to download the bundle" >&2
    exit 3
  fi
  mkdir -p "$CODEQL_HOME/dl" "$CODEQL_HOME/bundle"
  echo "Downloading CodeQL bundle ${CODEQL_VERSION} (${archive})"
  gh release download "codeql-bundle-v${CODEQL_VERSION}" -R github/codeql-action \
    -p "$archive" -p "${archive}.checksum.txt" -D "$CODEQL_HOME/dl" --clobber
  (cd "$CODEQL_HOME/dl" && sha256sum -c "${archive}.checksum.txt")
  tar -xzf "$CODEQL_HOME/dl/$archive" -C "$CODEQL_HOME/bundle"
  rm -f "$CODEQL_HOME/dl/$archive"
}

if [ ! -x "$bundle_codeql" ] && [ "$do_install" -eq 1 ]; then
  install_bundle
fi

if [ -x "$bundle_codeql" ]; then
  codeql_bin="$bundle_codeql"
elif command -v codeql >/dev/null 2>&1; then
  codeql_bin="$(command -v codeql)"
else
  cat >&2 <<EOF
codeql-local: CodeQL CLI not found.
Install the pinned bundle (${CODEQL_VERSION}) with:
  bash scripts/codeql-local.sh --install
or put a codeql binary on PATH.
EOF
  exit 3
fi

installed="$(env -u TMUX -u TMUX_TMPDIR "$codeql_bin" version --format=terse 2>/dev/null || true)"
echo "CodeQL CLI: ${installed:-unknown} (pinned ${CODEQL_VERSION})"
if [ -n "$installed" ] && [ "$installed" != "$CODEQL_VERSION" ]; then
  echo "note: installed CLI differs from the pinned version; results may not match default setup." >&2
fi

mkdir -p "$CODEQL_HOME/db" "$CODEQL_HOME/out"
rm -f "$CODEQL_HOME"/out/*.sarif

# Keep a CodeQL home that sits inside the source root out of extraction
# (the Python extractor would otherwise walk the bundle).
config_file="$CODEQL_HOME/codescanning-config.yml"
home_abs="$(cd "$CODEQL_HOME" && pwd)"
{
  echo "paths-ignore:"
  case "$home_abs" in
    "$ROOT"/*) echo "  - ${home_abs#"$ROOT"/}" ;;
  esac
  echo "  - .datawatch"
} > "$config_file"

run_codeql() {
  env -u TMUX -u TMUX_TMPDIR "$codeql_bin" "$@"
}

for lang in $languages; do
  pack="$(lang_pack "$lang")"
  suite="$(lang_suite "$lang")"
  build_mode="none"
  [ "$lang" = "go" ] && build_mode="autobuild"
  db="$CODEQL_HOME/db/$lang"
  sarif="$CODEQL_HOME/out/$lang.sarif"

  echo "== $lang: database create (build mode $build_mode)"
  t0=$(date +%s)
  if ! run_codeql database create "$db" --language="$lang" --build-mode="$build_mode" \
      --source-root=. --codescanning-config="$config_file" --overwrite; then
    echo "codeql-local: database create failed for $lang" >&2
    exit 4
  fi
  t1=$(date +%s)
  echo "== $lang: database analyze ($pack:codeql-suites/$suite)"
  if ! run_codeql database analyze "$db" "$pack:codeql-suites/$suite" \
      --format=sarifv2.1.0 --output="$sarif" --threads=0; then
    echo "codeql-local: database analyze failed for $lang" >&2
    exit 4
  fi
  t2=$(date +%s)
  echo "== $lang: elapsed create $((t1 - t0))s, analyze $((t2 - t1))s, total $((t2 - t0))s"
done

report_args=("$CODEQL_HOME/out")

if [ "$do_compare" -eq 1 ]; then
  alerts="$CODEQL_HOME/alerts.json"
  if ! gh_usable; then
    echo "note: gh is missing or not authenticated; continuing without the GitHub comparison." >&2
  else
    : > "$alerts"
    ok=1
    for state in open dismissed; do
      if ! gh api --paginate \
          "repos/{owner}/{repo}/code-scanning/alerts?ref=refs/heads/main&state=${state}&per_page=100" \
          >> "$alerts" 2>/dev/null; then
        ok=0
      fi
    done
    if [ "$ok" -eq 1 ]; then
      report_args+=(--alerts-json "$alerts")
    else
      echo "note: could not read the code-scanning alerts; continuing without the GitHub comparison." >&2
    fi
  fi
fi

rc=0
python3 -I scripts/codeql_local_report.py "${report_args[@]}" || rc=$?
exit "$rc"
