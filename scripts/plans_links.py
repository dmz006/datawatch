#!/usr/bin/env python3
"""Link-aware plan tidy for docs/plans/ (python3, standard library only).

A dated top-level plan (``YYYY-MM-DD-<slug>.md``) stays in ``docs/plans/``
as long as anything still references it:

  * an unclosed registry row in docs/plans/README.md,
  * any file under scripts/,
  * a root rules file (AGENT.md, CLAUDE.md, CONTEXT.md, CONTRIBUTING.md,
    DATAWATCH-CONTEXT.md, README.md, SECURITY.md, or any root *CONTEXT*.md),
  * any file under docs/ (outside docs/plans/),
  * the registry prose in docs/plans/README.md (everything that is not a
    numbered registry row).

Only plans past the age cutoff (``--days``, default 7) that have none of
those references and whose registry row is closed are moved to
``docs/plans/historical-plans/``.  When a plan moves, every relative and
absolute link pointing at it -- markdown link targets such as
``(2020-01-01-plan.md)`` and ``(plans/2020-01-01-plan.md)`` as well as
path mentions such as ``docs/plans/2020-01-01-plan.md`` -- is rewritten
in place in every referencing file, including the registry rows themselves.

A registry row is "closed" when its text carries a completion marker
(done / shipped / completed / merged / cancelled / landed) and is not
vetoed by a negation such as "not done" or "not started".  Rows without a
clear completion marker are treated as open: the linked plan stays.

Output is counts only (kept, archived, links rewritten) -- never a list
of files.  ``--dry-run`` computes the same counts without touching the
tree.  Moves use ``git mv`` when available and fall back to a plain
rename otherwise.

Usage:
  plans_links.py REPO_ROOT [--days N] [--dry-run]
"""
from __future__ import annotations

import argparse
import os
import re
import subprocess
import sys
import time

DATED_FILE_RE = re.compile(r"^\d{4}-\d{2}-\d{2}-.+\.md$")
ROW_RE = re.compile(r"^\|\s*\d+[A-Za-z]?\s*\|")
CLOSED_RE = re.compile(
    r"\b(?:done|shipped|completed|merged|cancelled|landed)\b", re.I
)
VETO_RE = re.compile(
    r"\bnot\s+(?:done|shipped|completed|started|yet|released|merged|implemented)\b"
    r"|\bnever\s+done\b",
    re.I,
)
RULE_FILE_RE = re.compile(
    r"^(?:agent|claude|contributing|readme|security)\.md$|context.*\.md$", re.I
)
MAX_BYTES = 2_000_000


def read_text(path: str) -> str | None:
    """Return the file contents, or None if it is not readable UTF-8 text."""
    try:
        if os.path.getsize(path) > MAX_BYTES:
            return None
        with open(path, encoding="utf-8") as fh:
            return fh.read()
    except (OSError, UnicodeDecodeError):
        return None


class PlansTidy:
    def __init__(self, root: str, days: int, dry_run: bool) -> None:
        self.root = root
        self.days = days
        self.dry_run = dry_run
        self.plans_dir = os.path.join(root, "docs", "plans")
        self.hist_dir = os.path.join(self.plans_dir, "historical-plans")
        self.registry = os.path.join(self.plans_dir, "README.md")

        self.plans: list[str] = []
        self.keep_sources: list[str] = []
        self.rewrite_sources: list[str] = []
        self.keep_refs: dict[str, int] = {}
        self.row_refs_closed: dict[str, int] = {}
        self.row_refs_open: dict[str, int] = {}
        self.body_refs: dict[str, int] = {}
        self._collect()

    # ── discovery ────────────────────────────────────────────────────────
    def _collect(self) -> None:
        if not os.path.isdir(self.plans_dir):
            return
        for name in sorted(os.listdir(self.plans_dir)):
            path = os.path.join(self.plans_dir, name)
            if DATED_FILE_RE.match(name) and os.path.isfile(path):
                self.plans.append(name)
        if not self.plans:
            return

        # root rules files
        for name in sorted(os.listdir(self.root)):
            path = os.path.join(self.root, name)
            if name.endswith(".md") and os.path.isfile(path) and RULE_FILE_RE.search(name):
                self.keep_sources.append(path)
        # scripts/
        self._add_tree(os.path.join(self.root, "scripts"), self.keep_sources)
        # docs/ outside docs/plans/
        docs_root = os.path.join(self.root, "docs")
        for dirpath, dirnames, filenames in os.walk(docs_root):
            if os.path.abspath(dirpath) == os.path.abspath(self.plans_dir):
                dirnames[:] = []
                continue
            for fn in sorted(filenames):
                self.keep_sources.append(os.path.join(dirpath, fn))
        self.keep_sources = sorted(set(self.keep_sources))

        # rewrite scope: every keep source plus the registry and the other
        # dated plans (cross-references inside docs/plans/ are rewritten too)
        sources = list(self.keep_sources)
        if os.path.isfile(self.registry):
            sources.append(self.registry)
        sources += [os.path.join(self.plans_dir, p) for p in self.plans]
        self.rewrite_sources = sorted(set(sources))

    def _add_tree(self, top: str, out: list[str]) -> None:
        if not os.path.isdir(top):
            return
        for dirpath, _dirnames, filenames in os.walk(top):
            for fn in sorted(filenames):
                out.append(os.path.join(dirpath, fn))

    # ── reference counting ───────────────────────────────────────────────
    def _count_references(self) -> None:
        for path in self.keep_sources:
            text = read_text(path)
            if not text:
                continue
            for plan in self.plans:
                n = text.count(plan)
                if n:
                    self.keep_refs[plan] = self.keep_refs.get(plan, 0) + n

        if os.path.isfile(self.registry):
            reg = read_text(self.registry) or ""
            for line in reg.splitlines():
                if ROW_RE.match(line):
                    if CLOSED_RE.search(line) and not VETO_RE.search(line):
                        bucket = self.row_refs_closed
                    else:
                        bucket = self.row_refs_open
                else:
                    bucket = self.body_refs
                for plan in self.plans:
                    n = line.count(plan)
                    if n:
                        bucket[plan] = bucket.get(plan, 0) + n

    # ── decision ─────────────────────────────────────────────────────────
    def _old_enough(self, plan: str) -> bool:
        try:
            epoch = int(time.mktime(time.strptime(plan[:10], "%Y-%m-%d")))
        except ValueError:
            return False
        return epoch < time.time() - self.days * 86400

    def _is_kept(self, plan: str) -> bool:
        if not self._old_enough(plan):
            return True
        if self.keep_refs.get(plan):
            return True
        if self.body_refs.get(plan):
            return True
        if self.row_refs_open.get(plan):
            return True
        return False

    # ── actions ──────────────────────────────────────────────────────────
    def _rewrite_links(self, plan: str) -> int:
        """Rewrite every link that points at plan; return replacement count."""
        escaped = re.escape(plan)
        # 1) any path that ends in plans/<plan> (relative or absolute)
        p1 = re.compile(r"((?:[A-Za-z0-9_.+-]+/)*plans/)" + escaped)

        def repl_p1(m: re.Match) -> str:
            prefix = m.group(1)
            if prefix.endswith("historical-plans/"):
                return m.group(0)
            return prefix + "historical-plans/" + plan

        # 2) bare name mentions (registry-style relative links); the look-behind
        #    keeps already-rewritten / directory-prefixed occurrences untouched
        p2 = re.compile(r"(?<![A-Za-z0-9_/.+-])" + escaped + r"(?![A-Za-z0-9_])")

        total = 0
        self_path = os.path.join(self.plans_dir, plan)
        for path in self.rewrite_sources:
            if path == self_path:
                continue
            text = read_text(path)
            if text is None or plan not in text:
                continue
            text, n1 = p1.subn(repl_p1, text)
            text, n2 = p2.subn("historical-plans/" + plan, text)
            if n1 + n2 == 0:
                continue
            if not self.dry_run:
                with open(path, "w", encoding="utf-8") as fh:
                    fh.write(text)
            total += n1 + n2
        return total

    def _move(self, plan: str) -> None:
        src = os.path.join(self.plans_dir, plan)
        dst = os.path.join(self.hist_dir, plan)
        os.makedirs(self.hist_dir, exist_ok=True)
        result = subprocess.run(
            ["git", "mv", "-f", src, dst],
            cwd=self.root,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        if result.returncode != 0:
            os.replace(src, dst)

    # ── run ──────────────────────────────────────────────────────────────
    def run(self) -> tuple[int, int, int]:
        self._count_references()
        archived = 0
        rewritten = 0
        for plan in self.plans:
            if self._is_kept(plan):
                continue
            rewritten += self._rewrite_links(plan)
            if not self.dry_run:
                self._move(plan)
            archived += 1
        return len(self.plans) - archived, archived, rewritten


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Link-aware tidy for docs/plans/ (counts only, no file lists)."
    )
    parser.add_argument("root", help="repository root (contains docs/plans/)")
    parser.add_argument("--days", type=int, default=7,
                        help="plan age threshold in days (default: 7)")
    parser.add_argument("--dry-run", action="store_true",
                        help="report counts without moving or rewriting anything")
    args = parser.parse_args(argv)

    root = os.path.abspath(args.root)
    if not os.path.isdir(os.path.join(root, "docs", "plans")):
        print("plans kept: 0")
        print("plans archived: 0")
        print("links rewritten: 0")
        if args.dry_run:
            print("(dry run — no files actually moved)")
        return 0

    kept, archived, rewritten = PlansTidy(root, args.days, args.dry_run).run()
    print(f"plans kept: {kept}")
    print(f"plans archived: {archived}")
    print(f"links rewritten: {rewritten}")
    if args.dry_run:
        print("(dry run — no files actually moved)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
