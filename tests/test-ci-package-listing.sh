#!/usr/bin/env bash
#
# test-ci-package-listing.sh
#
# Exercises the GHCR container-version listing code in
# .github/workflows/image-refresh.yaml ("Delete every throwaway recheck
# image") and .github/workflows/ghcr-cleanup.yaml ("List GHCR versions")
# without touching GitHub: a fake `gh` on PATH serves recorded fixture
# pages, and the real step `run:` blocks are extracted from the workflow
# YAML and executed as-is.
#
# The fixture (testdata/ci/ghcr-versions/page-1.json and page-2.json)
# follows the documented REST shape of
# /users/OWNER/packages/container/PKG/versions — objects with id, name,
# created_at and metadata.container.tags — and was written by hand, not
# captured live. It contains no real owner names and no tokens; the
# throwaway tag cve-recheck-900000001 sits on exactly one version (id
# 2002, page 2).
#
# All scratch state lives in a mktemp -d directory that a trap removes;
# nothing is written into the repository.
#
# Usage: bash tests/test-ci-package-listing.sh
# Output: one `ok <step>` or `FAIL <step> — reason` line per check, then
# PASS/FAIL; exits non-zero if any check failed.

set -uo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$root/testdata/ci/ghcr-versions/page"  # $fixture-{1,2} → pages
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

fails=0
ok()   { printf 'ok   %s\n' "$1"; }
fail() { printf 'FAIL %s — %s\n' "$1" "$2"; fails=$((fails+1)); }

# ---------------------------------------------------------------- fake gh --
bin="$work/bin"
mkdir -p "$bin"
cat > "$bin/gh" <<'FAKEGH'
#!/usr/bin/env bash
# Fake `gh` for tests/test-ci-package-listing.sh. Serves the recorded
# fixture pages for `gh api .../versions` listing calls and records DELETE
# calls. Reads FAKE_GH_PAGES_DIR, and FAKE_GH_DELETE_LOG /
# FAKE_GH_FAIL_PKG from the environment (set by the test).
#
# It deliberately rejects unknown flags the same way the real gh does
# (exit 1, "unknown flag: <flag>"): `--slurp` is not a gh api flag, and a
# fake that accepts and emulates it would hide a workflow that breaks on
# the installed gh.
set -u
if [[ "${1:-}" != "api" ]]; then
  echo "fake gh: unsupported command: ${*:-(none)}" >&2
  exit 3
fi
shift
method="GET"
path=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -X) method="${2:?-X needs a value}"; shift 2 ;;
    --paginate) shift ;;
    --*) echo "unknown flag: $1" >&2; exit 1 ;;
    *) if [[ -z "$path" && "$1" == /* ]]; then path="$1"; shift
       else echo "fake gh: unsupported argument: $1" >&2; exit 3
       fi ;;
  esac
done
[[ -n "$path" ]] || { echo "fake gh: no api path given" >&2; exit 3; }

# A named failing package: both user and org endpoints return 404, as
# they do for a package that does not exist under that owner.
if [[ -n "${FAKE_GH_FAIL_PKG:-}" && "$path" == *"/$FAKE_GH_FAIL_PKG/"* ]]; then
  echo "HTTP 404 Not Found" >&2
  exit 1
fi

if [[ "$method" == "DELETE" ]]; then
  echo "$path" >> "${FAKE_GH_DELETE_LOG:?FAKE_GH_DELETE_LOG must be set}"
  exit 0
fi

# GET listing: serve the two fixture pages the way `gh api --paginate`
# actually does: the pages are concatenated arrays, one JSON document per
# page (there is no --slurp-style folding; the workflow folds with jq -s).
cat "${FAKE_GH_PAGES_DIR:?FAKE_GH_PAGES_DIR must be set}"-1.json
cat "${FAKE_GH_PAGES_DIR:?FAKE_GH_PAGES_DIR must be set}"-2.json
FAKEGH
chmod +x "$bin/gh"

# ------------------------------------------- extract a step's run block --
extract="$work/extract-run.py"
cat > "$extract" <<'PYEOF'
import sys, yaml
wf = yaml.safe_load(open(sys.argv[1], encoding="utf-8"))
want = sys.argv[2]
for job in (wf.get("jobs") or {}).values():
    for step in (job.get("steps") or []):
        if step.get("name") == want and "run" in step:
            sys.stdout.write(step["run"])
            sys.exit(0)
sys.stderr.write("step not found: %s\n" % want)
sys.exit(4)
PYEOF

# --------------------------------------------------- (a) done: fake gh --
# ----------------------------------------------------- (b) image-refresh --
runb="$work/delete-step.sh"
if ! python3 "$extract" "$root/.github/workflows/image-refresh.yaml" \
      "Delete every throwaway recheck image" > "$runb"; then
  fail b-extract "could not extract the delete step from image-refresh.yaml"
  runb=""
fi
if [[ -n "$runb" && -s "$runb" ]]; then
  delog="$work/delete.log"
  : > "$delog"
  outb="$work/b.out"
  if env PATH="$bin:$PATH" \
      OWNER=dmz006 \
      TAG=cve-recheck-900000001 \
      GH_TOKEN=dummy-token-not-a-real-secret \
      FAKE_GH_FAIL_PKG=datawatch-validator \
      FAKE_GH_DELETE_LOG="$delog" \
      FAKE_GH_PAGES_DIR="$fixture" \
      GITHUB_STEP_SUMMARY="$work/step-summary" \
      bash "$runb" > "$outb" 2> "$work/b.err"; then
    ok "b: delete step exits 0 with a failing package"
  else
    fail "b: delete step exit" "$(head -3 "$work/b.err" | tr '\n' ' ')"
  fi

  n2002=$(grep -c '/versions/2002$' "$delog" 2>/dev/null || true)
  if [[ "${n2002:-0}" -eq 9 ]]; then
    ok "b: DELETE logged for /versions/2002 on every other package ($n2002)"
  else
    fail "b: DELETE log for /versions/2002" "expected 9 lines, got ${n2002:-0}"
  fi

  if ! grep -q 'datawatch-validator' "$delog"; then
    ok "b: no DELETE targets datawatch-validator"
  else
    fail "b: no DELETE for datawatch-validator" "$(tr '\n' ';' < "$delog")"
  fi

  if grep -Eq '::warning::.*datawatch-validator' "$outb"; then
    ok "b: ::warning:: names datawatch-validator"
  else
    fail "b: ::warning:: for datawatch-validator" "$(tr '\n' ';' < "$outb" | head -c 300)"
  fi
fi

# --------------------------------------------------- (c) reproduces bug --
# The old pipeline handed `gh api --paginate` (no --slurp) output to jq:
# without --slurp the pages are concatenated arrays, so the only way to
# fold them into one document was wrapping them in one outer array; with
# two array-typed pages that is a parse error ("Expected separator
# between values"), which a tag-selecting `jq -r` then surfaces.
pages="$(env PATH="$bin:$PATH" FAKE_GH_PAGES_DIR="$fixture" \
  gh api /users/dmz006/packages/container/datawatch-agent-base/versions \
  --paginate 2>/dev/null)"
errc="$(printf '[ %s ]\n' "$pages" \
  | jq -r --arg tag cve-recheck-900000001 '
      [.[][]]
      | map(select(((.metadata? | type) == "object") and
                   ((.metadata.container.tags // []) | index($tag)) != null))
      | .[0].id' 2>&1)"
if printf '%s' "$errc" | grep -q 'parse error' && \
   printf '%s' "$errc" | grep -q 'Expected separator between values'; then
  ok "c: old pipeline fails with a parse error (fixture reproduces bug)"
else
  fail "c: old pipeline parse error" "got: $(printf '%s' "$errc" | head -1)"
fi

# ------------------------------------------------- (d) ghcr-cleanup list --
rund="$work/list-step.sh"
if ! python3 "$extract" "$root/.github/workflows/ghcr-cleanup.yaml" \
      "List GHCR versions" > "$rund"; then
  fail d-extract "could not extract the list step from ghcr-cleanup.yaml"
  rund=""
fi
if [[ -n "$rund" && -s "$rund" ]]; then
  dir_d="$work/d"
  mkdir -p "$dir_d"
  if (
    cd "$dir_d" || exit 9
    env PATH="$bin:$PATH" \
        PKG=datawatch-agent-base \
        OWNER=dmz006 \
        GH_TOKEN=dummy-token-not-a-real-secret \
        FAKE_GH_PAGES_DIR="$fixture" \
        bash "$rund"
  ) > "$work/d.out" 2> "$work/d.err"; then
    ok "d: list step exits 0"
  else
    fail "d: list step exit" "$(head -3 "$work/d.err" | tr '\n' ' ')"
  fi

  len=$(jq 'length' "$dir_d/versions.json" 2>/dev/null)
  if [[ "$len" == "5" ]]; then
    ok "d: versions.json length is 5 (pages flattened)"
  else
    fail "d: versions.json length" "expected 5, got ${len:-<missing>}"
  fi

  if ( cd "$dir_d" && python3 -c 'import json; json.load(open("versions.json"))' ); then
    ok "d: python3 json.load succeeds on versions.json"
  else
    fail "d: json.load" "versions.json is not valid JSON"
  fi
fi

# ------------------------------------------------------- (e) no --slurp --
# `--slurp` is not a gh api flag on the installed gh (it exits with
# "unknown flag: --slurp"); fail loudly if the workflows call for it,
# instead of waiting for a live run to break.
slurps="$(grep -n -- '--slurp' "$root/.github/workflows/ghcr-cleanup.yaml" \
           "$root/.github/workflows/image-refresh.yaml" 2>/dev/null \
           | grep -v ':[0-9]*: *#')"
slurps="${slurps:-}"
if [[ -n "$slurps" ]]; then
  fail e-no-slurp "workflows use the nonexistent --slurp flag: $(tr '\n' ';' <<<"$slurps")"
else
  ok "e: no --slurp in the workflows"
fi

# ------------------------------------------------------------------ done --
if [[ $fails -eq 0 ]]; then
  echo PASS
  exit 0
fi
echo "FAIL ($fails check(s) failed)"
exit 1
