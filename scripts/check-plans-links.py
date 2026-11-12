#!/usr/bin/env python3
"""Dangling plan-link checker (python3, standard library only).

Walks the same reference surfaces that scripts/tidy-plans.sh rewrites --
every file under scripts/, the root rules files (AGENT.md, CLAUDE.md,
CONTEXT.md, CONTRIBUTING.md, DATAWATCH-CONTEXT.md, README.md, SECURITY.md,
or any root *CONTEXT*.md), and every file under docs/ -- and flags any
markdown link target that names a dated plan (``YYYY-MM-DD-<slug>.md``)
which cannot be resolved against the docs/plans/ tree (the top level,
historical-plans/, or any other subdirectory).  Plain prose mentions of
plan names are notes, not links, and are not flagged; remote
http(s) link targets are not checked because they point outside the
repository.

Output is a single count (dangling plan references) -- never a list of
files.  Exit 0 when the count is zero, 1 otherwise.

Usage:
  check-plans-links.py REPO_ROOT [--quiet]
"""
from __future__ import annotations

import os
import re
import sys

PLAN_NAME_RE = re.compile(r"\d{4}-\d{2}-\d{2}-[A-Za-z0-9_]+(?:-[A-Za-z0-9_]+)*\.md")
MD_LINK_RE = re.compile(r"\]\(\s*<?([^)<\s]+)>?")
RULE_FILE_RE = re.compile(
    r"^(?:agent|claude|contributing|readme|security)\.md$|context.*\.md$", re.I
)
MAX_BYTES = 2_000_000


def read_text(path: str) -> str | None:
    try:
        if os.path.getsize(path) > MAX_BYTES:
            return None
        with open(path, encoding="utf-8") as fh:
            return fh.read()
    except (OSError, UnicodeDecodeError):
        return None


def iter_tree(top: str) -> list[str]:
    out = []
    if not os.path.isdir(top):
        return out
    for dirpath, _dirnames, filenames in os.walk(top):
        for fn in sorted(filenames):
            out.append(os.path.join(dirpath, fn))
    return out


def main(argv: list[str]) -> int:
    quiet = "--quiet" in argv
    rest = [a for a in argv if a != "--quiet"]
    root = os.path.abspath(rest[0] if rest else ".")

    plans_dir = os.path.join(root, "docs", "plans")
    if not os.path.isdir(plans_dir):
        if not quiet:
            print("dangling plan references: 0")
        return 0

    # every plan-side file that exists counts as a resolvable target,
    # keyed by both leaf name and repo-relative path
    known_names: set[str] = set()
    known_paths: set[str] = set()
    for dirpath, _dirnames, filenames in os.walk(plans_dir):
        for fn in filenames:
            known_names.add(fn)
            known_paths.add(os.path.relpath(os.path.join(dirpath, fn), root))
            known_paths.add(os.path.relpath(os.path.join(dirpath, fn), plans_dir))

    sources: list[str] = []
    for name in sorted(os.listdir(root)):
        path = os.path.join(root, name)
        if name.endswith(".md") and os.path.isfile(path) and RULE_FILE_RE.search(name):
            sources.append(path)
    sources += iter_tree(os.path.join(root, "scripts"))
    sources += iter_tree(os.path.join(root, "docs"))
    sources = sorted(set(sources))

    dangling = 0
    for path in sources:
        text = read_text(path)
        if not text:
            continue
        for target in MD_LINK_RE.findall(text):
            tracked = any(f"{d}/" in target for d in ("reports", "parity-issues"))
            if PLAN_NAME_RE.search(target) is None and not tracked:
                continue
            if re.match(r"^[a-z][a-z0-9+.-]*:", target):
                continue  # remote / absolute URL scheme
            if target.startswith(("#", "//")):
                continue
            clean = target.split("#", 1)[0]
            if not clean.endswith(".md"):
                continue
            if tracked and PLAN_NAME_RE.search(clean) is None and not clean.endswith(".md"):
                continue
            rel = clean.lstrip("./")
            leaf = clean.rsplit("/", 1)[-1]
            if (
                leaf in known_names
                or rel in known_paths
                or os.path.exists(os.path.join(root, clean))
                or os.path.exists(os.path.join(os.path.dirname(path), clean))
            ):
                continue
            dangling += 1

    if not quiet or dangling:
        print(f"dangling plan references: {dangling}")
    return 1 if dangling else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
