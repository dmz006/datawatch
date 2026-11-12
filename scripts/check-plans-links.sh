#!/usr/bin/env bash
#
# check-plans-links.sh — count references (markdown link targets,
# script references, and root rules-file references) that point into
# docs/plans/ and whose target cannot be resolved, and print only that
# count.  Exits 0 and prints 0 when every target resolves; exits non-zero
# when any target is missing.
#
# Discovery and file reading are shared with scripts/plans_links.py:
# the same reference surfaces it rewrites are scanned — files under
# scripts/ (excluding test-fixture scripts whose heredocs quote plan
# paths), the root rules files (AGENT.md, CLAUDE.md, any *CONTEXT*.md,
# CONTRIBUTING.md, README.md, SECURITY.md), and files under docs/.
#
# Only markdown link targets are treated as links — prose mentions of
# plan paths (inline code, examples) are not counted, and remote
# http(s) targets are not checked because they point outside this
# repository.  Broken targets are never listed — only the count.
#
# Usage:
#   scripts/check-plans-links.sh           exit non-zero if any target is missing
#   scripts/check-plans-links.sh --quiet   stay silent when the count is zero

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
ROOT=$(git rev-parse --show-toplevel 2>/dev/null || dirname "$SCRIPT_DIR")

exec python3 - "$ROOT" "$@" <<'PY'
import os
import re
import sys

quiet = "--quiet" in sys.argv
rest = [a for a in sys.argv[1:] if a != "--quiet"]
root = os.path.abspath(rest[0]) if rest else os.getcwd()

# Shared discovery, rules-file vocabulary, and file reader from
# scripts/plans_links.py.
sys.path.insert(0, os.path.join(root, "scripts"))
try:
    import plans_links as pl
except ImportError:
    sys.path.insert(0, os.path.join(os.getcwd(), "scripts"))
    import plans_links as pl

plans_dir = os.path.join(root, "docs", "plans")


def finish(n: int) -> int:
    if not quiet or n:
        print(n)
    return 1 if n else 0


if not os.path.isdir(plans_dir):
    sys.exit(finish(0))

# Reference surfaces: everything plans_links.py rewrites (scripts/,
# root rules files, docs/ including the registry and cross-references).
try:
    tidy = pl.PlansTidy(root, days=7, dry_run=True)
    sources = sorted(set(tidy.keep_sources) | set(tidy.rewrite_sources))
except Exception:
    sources = []
if not sources:
    # no dated plans -> PlansTidy collected nothing; collect the same
    # surfaces directly with the shared rules-file vocabulary
    for name in sorted(os.listdir(root)):
        path = os.path.join(root, name)
        if name.endswith(".md") and os.path.isfile(path) and pl.RULE_FILE_RE.search(name):
            sources.append(path)
    for top in ("scripts", "docs"):
        topdir = os.path.join(root, top)
        if not os.path.isdir(topdir):
            continue
        for dirpath, _dirnames, filenames in os.walk(topdir):
            for fn in sorted(filenames):
                sources.append(os.path.join(dirpath, fn))
    sources = sorted(set(sources))


def is_fixture(path: str) -> bool:
    # test scripts quote plan paths inside heredocs/python fixtures;
    # those are test scaffolding, not live links
    name = os.path.basename(path)
    if path.startswith(os.path.join(root, "scripts") + os.sep):
        return name.startswith("test-") or "-live-check.sh" in name
    return False


SCHEME_RE = re.compile(r"^[A-Za-z][A-Za-z0-9+.\-]*:")
DATED_LEAF_RE = re.compile(r"^\d{4}-\d{2}-\d{2}-[A-Za-z0-9_]+(?:-[A-Za-z0-9_]+)*\.md$")
MD_TARGET_RE = re.compile(r"\]\(\s*<?([^)<\s]+)>?")


TRACKED_DIRS = ("reports", "parity-issues")


def archived_leaf_exists(leaf: str) -> bool:
    # reports/ and parity-issues/ hold archive/YYYY-MM/ subfolders; a
    # target counts as present when its leaf lives in any of them
    for d in TRACKED_DIRS:
        base = os.path.join(plans_dir, d)
        if os.path.exists(os.path.join(base, leaf)):
            return True
        arch = os.path.join(base, "archive")
        if os.path.isdir(arch):
            for sub in os.listdir(arch):
                if os.path.exists(os.path.join(arch, sub, leaf)):
                    return True
    return False


def is_plans_target(clean: str) -> bool:
    if not clean.endswith(".md"):
        return False
    if "plans/" in clean or any(f"{d}/" in clean for d in TRACKED_DIRS):
        return True
    return bool(DATED_LEAF_RE.match(clean.rsplit("/", 1)[-1]))


def resolvable(clean: str, srcdir: str) -> bool:
    if srcdir and os.path.exists(os.path.join(srcdir, clean)):
        return True
    if os.path.exists(os.path.join(root, clean)):
        return True
    if clean.startswith("plans/"):
        if os.path.exists(os.path.join(root, "docs", clean)):
            return True
    if os.path.exists(os.path.join(plans_dir, clean)):
        return True
    leaf = clean.rsplit("/", 1)[-1]
    if any(f"{d}/" in clean for d in TRACKED_DIRS) or DATED_LEAF_RE.match(leaf):
        if archived_leaf_exists(leaf):
            return True
    if DATED_LEAF_RE.match(leaf):
        for base in (plans_dir, os.path.join(plans_dir, "historical-plans")):
            if os.path.exists(os.path.join(base, leaf)):
                return True
    return False


broken = 0
for path in sources:
    if is_fixture(path):
        continue
    text = pl.read_text(path)
    if not text:
        continue
    for target in MD_TARGET_RE.findall(text):
        if SCHEME_RE.match(target) or target.startswith(("#", "//")):
            continue  # remote or in-page: outside the repository
        clean = target.split("#", 1)[0]
        if not is_plans_target(clean):
            continue
        if not resolvable(clean, os.path.dirname(path)):
            broken += 1

sys.exit(finish(broken))
PY
