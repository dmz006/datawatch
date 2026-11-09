#!/usr/bin/env python3
"""Monthly archive for docs/plans/reports/ and docs/plans/parity-issues/.

Top-level *.md files directly in those two folders move into
docs/plans/<folder>/archive/YYYY-MM/ once they are old enough.  README.md and
anything already under an archive/ subfolder are never touched.

Age of a file (the date that picks its YYYY-MM folder):
  1. the YYYY-MM-DD name prefix, if it has one;
  2. else the committer date of its last git commit;
  3. else its modification time.
A file is old enough when its age is at least --days (default 7) days.

An old file stays in place when any of these references it, by basename:
  * an open (not closed) row of docs/plans/README.md;
  * any file under scripts/;
  * a root rules file (plans_links.RULE_FILE_RE: AGENT.md, CLAUDE.md,
    CONTEXT.md, CONTRIBUTING.md, DATAWATCH-CONTEXT.md, README.md, SECURITY.md);
  * any file under docs/ outside docs/plans/.

Parity-issue drafts move only once filed: the file has a "Filed issue" section
that records an issue URL or number, and the file carries no "not filed",
"not yet filed" or "not needed" marker.  An old, unreferenced, unfiled draft is
counted as unclassified and is never moved.

Every link to a moved file is rewritten in the registry, the rules files,
scripts/ and docs/ (markdown link targets, reference definitions, href/src
attributes and docs/plans/<folder>/<name> path mentions).  A moved file's own
relative links are re-based so they still resolve from the deeper folder.

The registry-row and rules-file rules are imported from plans_links.py, so
there is one definition of each.

Output is counts only, one per line (never file names).  --list-unclassified
prints the unclassified file names instead, one per line, and nothing else; it
never changes anything.  --dry-run prints the same counts and changes nothing.
Moves use git mv for tracked files and a plain move for untracked files.

Usage:
  plans_monthly_archive.py <root> [--dry-run] [--days N] [--list-unclassified]
"""
from __future__ import annotations

import argparse
import datetime as dt
import os
import posixpath
import re
import subprocess
import sys
from dataclasses import dataclass, field

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from plans_links import (  # noqa: E402  (shared rules, defined once)
    CLOSED_RE,
    ROW_RE,
    RULE_FILE_RE,
    VETO_RE,
    read_text,
)

FOLDERS = ("reports", "parity-issues")
SKIP_NAMES = frozenset({"README.md"})
DATE_PREFIX_RE = re.compile(r"^(\d{4})-(\d{2})-(\d{2})-")
FILED_TITLE_RE = re.compile(r"^(#{1,6})\s*filed issue\s*$", re.I)
HEADING_RE = re.compile(r"^(#{1,6})\s+\S")
ISSUE_REF_RE = re.compile(
    r"https?://\S+/(?:issues|pull)/\d+"          # issue URL
    r"|\bissue\b[^\n\d]{0,20}\d+"                # "Issue: 55", "Issue number: 55"
    r"|(?<![\w&])#\d+\b",                        # "#55"
    re.I,
)
NOT_FILED_RE = re.compile(r"\bnot\s+(?:yet\s+)?filed\b|\bnot\s+needed\b", re.I)
SCHEME_RE = re.compile(r"^[A-Za-z][A-Za-z0-9+.-]*:")
# link targets: markdown "](target", reference definitions "[x]: target",
# and href/src attributes.  Group "pre" is the text before the target.
TARGET_RE = re.compile(
    r"(?P<pre>\]\(\s*<?|^[ ]{0,3}\[[^\]\n]+\]:[ \t]*|\b(?:href|src)=[\"']?)"
    r"(?P<target>[^\s)<>\"']+)",
    re.M,
)


@dataclass(frozen=True)
class Candidate:
    folder: str
    name: str
    path: str


@dataclass
class Plan:
    stats: dict[str, int] = field(default_factory=dict)
    moves: dict[str, str] = field(default_factory=dict)  # old rel -> new rel
    changes: dict[str, str] = field(default_factory=dict)  # rel -> new text
    links: int = 0
    unclassified: list[str] = field(default_factory=list)

    def bump(self, key: str) -> None:
        self.stats[key] = self.stats.get(key, 0) + 1


def _iter_files(top: str):
    for dirpath, dirnames, filenames in os.walk(top):
        dirnames.sort()
        for fn in sorted(filenames):
            yield os.path.join(dirpath, fn)


class Archiver:
    def __init__(self, root: str, days: int, today: dt.date | None = None) -> None:
        self.root = os.path.abspath(root)
        self.days = days
        self.today = today or dt.date.today()
        self.plans_dir = os.path.join(self.root, "docs", "plans")
        self.tracked = self._tracked_under_plans()

    # ── paths ────────────────────────────────────────────────────────────
    def _rel(self, path: str) -> str:
        return posixpath.normpath(os.path.relpath(path, self.root).replace(os.sep, "/"))

    def _abs(self, rel: str) -> str:
        return os.path.join(self.root, *rel.split("/"))

    def _tracked_under_plans(self) -> set[str]:
        try:
            out = subprocess.run(
                ["git", "ls-files", "-z", "--", "docs/plans"],
                cwd=self.root, capture_output=True, check=False,
            )
        except OSError:
            return set()
        if out.returncode != 0:
            return set()
        return {p for p in out.stdout.decode("utf-8", "replace").split("\0") if p}

    # ── discovery ────────────────────────────────────────────────────────
    def _candidates(self) -> list[Candidate]:
        found: list[Candidate] = []
        for folder in FOLDERS:
            top = os.path.join(self.plans_dir, folder)
            if not os.path.isdir(top):
                continue
            for name in sorted(os.listdir(top)):
                path = os.path.join(top, name)
                if name in SKIP_NAMES or not name.endswith(".md") or not os.path.isfile(path):
                    continue
                found.append(Candidate(folder, name, path))
        return found

    def _keep_corpus(self) -> list[str]:
        """Text that can keep a file in place: open registry rows, rules
        files, everything under scripts/, and everything under docs/ outside
        docs/plans/."""
        texts: list[str] = []
        registry = os.path.join(self.plans_dir, "README.md")
        if os.path.isfile(registry):
            for line in (read_text(registry) or "").splitlines():
                if ROW_RE.match(line) and not (
                    CLOSED_RE.search(line) and not VETO_RE.search(line)
                ):
                    texts.append(line)
        for name in sorted(os.listdir(self.root)):
            path = os.path.join(self.root, name)
            if os.path.isfile(path) and RULE_FILE_RE.search(name):
                texts.append(read_text(path) or "")
        for path in _iter_files(os.path.join(self.root, "scripts")):
            text = read_text(path)
            if text:
                texts.append(text)
        docs = os.path.join(self.root, "docs")
        for dirpath, dirnames, filenames in os.walk(docs):
            dirnames[:] = sorted(d for d in dirnames
                                 if os.path.join(dirpath, d) != self.plans_dir)
            for fn in filenames:
                text = read_text(os.path.join(dirpath, fn))
                if text:
                    texts.append(text)
        return texts

    def _scope(self) -> list[str]:
        """Files whose links are rewritten: rules files, scripts/, docs/."""
        rels: set[str] = set()
        for name in os.listdir(self.root):
            if os.path.isfile(os.path.join(self.root, name)) and RULE_FILE_RE.search(name):
                rels.add(name)
        for top in ("scripts", "docs"):
            for path in _iter_files(os.path.join(self.root, top)):
                rels.add(self._rel(path))
        return sorted(rels)

    # ── per-file facts ───────────────────────────────────────────────────
    def _git_date(self, path: str) -> dt.date | None:
        try:
            out = subprocess.run(
                ["git", "log", "-1", "--format=%cI", "--", self._rel(path)],
                cwd=self.root, capture_output=True, text=True, check=False,
            )
        except OSError:
            return None
        stamp = out.stdout.strip()
        if out.returncode != 0 or not stamp:
            return None
        try:
            return dt.datetime.fromisoformat(stamp).date()
        except ValueError:
            return None

    def _age(self, cand: Candidate) -> dt.date:
        m = DATE_PREFIX_RE.match(cand.name)
        if m:
            try:
                return dt.date(int(m[1]), int(m[2]), int(m[3]))
            except ValueError:
                pass
        git_day = self._git_date(cand.path)
        if git_day:
            return git_day
        return dt.date.fromtimestamp(os.path.getmtime(cand.path))

    @staticmethod
    def _is_filed(text: str) -> bool:
        """True when a "Filed issue" section records an issue URL or number and
        the file carries no not-filed marker anywhere."""
        if NOT_FILED_RE.search(text):
            return False
        lines = text.splitlines()
        for i, line in enumerate(lines):
            title = FILED_TITLE_RE.match(line)
            if not title:
                continue
            level = len(title.group(1))
            section: list[str] = []
            for nxt in lines[i + 1:]:
                heading = HEADING_RE.match(nxt)
                if heading and len(heading.group(1)) <= level:
                    break
                section.append(nxt)
            if ISSUE_REF_RE.search("\n".join(section)):
                return True
        return False

    # ── reference matching ───────────────────────────────────────────────
    @staticmethod
    def _referenced_names(names: set[str], corpus: list[str]) -> set[str]:
        if not names:
            return set()
        alt = "|".join(re.escape(n) for n in sorted(names, key=len, reverse=True))
        pattern = re.compile(r"(?<![A-Za-z0-9_.+-])(" + alt + r")(?![A-Za-z0-9_])")
        hits: set[str] = set()
        for text in corpus:
            hits.update(pattern.findall(text))
        return hits

    # ── link rewriting ───────────────────────────────────────────────────
    def _retarget(self, moves: dict[str, str], old_dir: str, new_dir: str,
                  target: str) -> str | None:
        """New spelling of a relative link target, or None when unchanged."""
        if not target or target[0] in "#/" or SCHEME_RE.match(target):
            return None
        cut = len(target)
        for sep in "#?":
            idx = target.find(sep)
            if idx != -1:
                cut = min(cut, idx)
        path, suffix = target[:cut], target[cut:]
        if not path:
            return None
        old_target = posixpath.normpath(posixpath.join(old_dir, path))
        mapped = moves.get(old_target, old_target)
        if mapped == old_target and new_dir == old_dir:
            return None
        new_path = posixpath.relpath(mapped, new_dir)
        if path.endswith("/") and not new_path.endswith("/"):
            new_path += "/"
        if new_path == path:
            return None
        return new_path + suffix

    def _rewrite(self, text: str, src_rel: str, moves: dict[str, str],
                 mention_re: re.Pattern | None, mention_map: dict[tuple[str, str], str],
                 is_md: bool) -> tuple[str, int]:
        count = 0
        old_dir = posixpath.dirname(src_rel)
        new_dir = posixpath.dirname(moves.get(src_rel, src_rel))

        if is_md:
            def repl_target(m: re.Match) -> str:
                nonlocal count
                target = m.group("target")
                new = self._retarget(moves, old_dir, new_dir, target)
                if new is None:
                    return m.group(0)
                count += 1
                return m.group("pre") + new

            text = TARGET_RE.sub(repl_target, text)

        if mention_re is not None:
            def repl_mention(m: re.Match) -> str:
                nonlocal count
                key = (m.group(1), m.group(2))
                if key not in mention_map:
                    return m.group(0)
                count += 1
                return mention_map[key]

            text = mention_re.sub(repl_mention, text)
        return text, count

    # ── planning ─────────────────────────────────────────────────────────
    def plan(self) -> Plan:
        plan = Plan()
        cands = self._candidates()
        referenced = self._referenced_names(
            {c.name for c in cands}, self._keep_corpus()
        )

        archive_of: dict[tuple[str, str], str] = {}
        for cand in cands:
            label = "reports" if cand.folder == "reports" else "parity issues"
            age = self._age(cand)
            old_enough = (self.today - age).days >= self.days
            if not old_enough or cand.name in referenced:
                plan.bump(f"{label} kept")
                continue
            if cand.folder == "parity-issues":
                with open(cand.path, encoding="utf-8", errors="replace") as fh:
                    if not self._is_filed(fh.read()):
                        plan.bump("unclassified")
                        plan.unclassified.append(cand.name)
                        continue
            new_rel = f"docs/plans/{cand.folder}/archive/{age:%Y-%m}/{cand.name}"
            if os.path.exists(self._abs(new_rel)):
                plan.bump(f"{label} kept")
                continue
            old_rel = self._rel(cand.path)
            plan.moves[old_rel] = new_rel
            archive_of[(cand.folder, cand.name)] = new_rel
            plan.bump(f"{label} archived")

        # path mentions docs/plans/<folder>/<name> for moved files only
        mention_map: dict[tuple[str, str], str] = {}
        mention_re: re.Pattern | None = None
        if archive_of:
            names = sorted({n for _, n in archive_of}, key=len, reverse=True)
            alt = "|".join(re.escape(n) for n in names)
            mention_re = re.compile(
                r"(?<![A-Za-z0-9_.+-])plans/(reports|parity-issues)/(" + alt
                + r")(?![A-Za-z0-9_])"
            )
            for (folder, name), new_rel in archive_of.items():
                mention_map[(folder, name)] = "plans/" + new_rel[len("docs/plans/"):]

        for rel in self._scope():
            text = read_text(self._abs(rel))
            if not text:
                continue
            new_text, n = self._rewrite(
                text, rel, plan.moves, mention_re, mention_map,
                is_md=rel.endswith(".md"),
            )
            if n:
                plan.changes[rel] = new_text
                plan.links += n

        plan.unclassified.sort()
        return plan

    # ── execution ────────────────────────────────────────────────────────
    def apply(self, plan: Plan) -> None:
        for rel, text in plan.changes.items():
            with open(self._abs(rel), "w", encoding="utf-8") as fh:
                fh.write(text)
        for old_rel, new_rel in plan.moves.items():
            src, dst = self._abs(old_rel), self._abs(new_rel)
            os.makedirs(os.path.dirname(dst), exist_ok=True)
            if old_rel in self.tracked:
                done = subprocess.run(
                    ["git", "mv", src, dst], cwd=self.root,
                    capture_output=True, check=False,
                )
                if done.returncode == 0:
                    continue
            os.replace(src, dst)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Monthly archive for docs/plans/reports/ and parity-issues/ "
                    "(counts only, no file names)."
    )
    parser.add_argument("root", help="repository root (contains docs/plans/)")
    parser.add_argument("--dry-run", action="store_true",
                        help="print the same counts and change nothing")
    parser.add_argument("--days", type=int, default=7,
                        help="minimum file age in days (default: 7)")
    parser.add_argument("--list-unclassified", action="store_true",
                        help="print unclassified file names only; change nothing")
    args = parser.parse_args(argv)
    if args.days < 0:
        parser.error("--days must be zero or more")

    archiver = Archiver(args.root, args.days)
    plan = archiver.plan()

    if args.list_unclassified:
        for name in plan.unclassified:
            print(name)
        return 0

    if not args.dry_run:
        archiver.apply(plan)

    stats = plan.stats
    print(f"reports kept: {stats.get('reports kept', 0)}")
    print(f"reports archived: {stats.get('reports archived', 0)}")
    print(f"parity issues kept: {stats.get('parity issues kept', 0)}")
    print(f"parity issues archived: {stats.get('parity issues archived', 0)}")
    print(f"links rewritten: {plan.links}")
    print(f"unclassified: {stats.get('unclassified', 0)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
