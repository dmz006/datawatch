#!/usr/bin/env python3
"""Tests for scripts/plans_monthly_archive.py.

Every case builds its own throwaway tree under tests/.archive-work/ (inside
the project, never /tmp and never the real docs/plans/), runs the real CLI,
and removes the tree afterwards.

Run:  python3 tests/test_plans_monthly_archive.py
"""
from __future__ import annotations

import datetime as dt
import os
import posixpath
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
SCRIPT = REPO / "scripts" / "plans_monthly_archive.py"
WORK = REPO / "tests" / ".archive-work"

sys.path.insert(0, str(REPO / "scripts"))
import plans_links  # noqa: E402
import plans_monthly_archive as pma  # noqa: E402

TODAY = dt.date.today()
YOUNG = TODAY.isoformat()   # name prefix for a file younger than 7 days
OLD = "2026-10-01"          # far past the default 7-day cutoff

COUNT_KEYS = (
    "reports kept", "reports archived", "parity issues kept",
    "parity issues archived", "links rewritten", "unclassified",
)


class ArchiveCase(unittest.TestCase):
    def setUp(self) -> None:
        WORK.mkdir(parents=True, exist_ok=True)
        self.root = Path(tempfile.mkdtemp(prefix="case-", dir=WORK))
        self.addCleanup(self._cleanup)

    def _cleanup(self) -> None:
        shutil.rmtree(self.root, ignore_errors=True)
        try:
            WORK.rmdir()
        except OSError:
            pass

    # ── helpers ──────────────────────────────────────────────────────────
    def put(self, rel: str, text: str = "# plan\n", mtime: float | None = None) -> Path:
        path = self.root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")
        if mtime is not None:
            os.utime(path, (mtime, mtime))
        return path

    def tool(self, *extra: str) -> subprocess.CompletedProcess:
        return subprocess.run(
            [sys.executable, str(SCRIPT), str(self.root), *extra],
            capture_output=True, text=True, check=False,
        )

    def counts(self, *extra: str) -> dict[str, int]:
        result = self.tool(*extra)
        self.assertEqual(result.returncode, 0, result.stderr)
        out: dict[str, int] = {}
        for line in result.stdout.splitlines():
            key, _, value = line.rpartition(": ")
            out[key] = int(value)
        return out

    def exists(self, rel: str) -> bool:
        return (self.root / rel).is_file()

    def read(self, rel: str) -> str:
        return (self.root / rel).read_text(encoding="utf-8")

    def snapshot(self) -> dict[str, bytes]:
        return {
            str(p.relative_to(self.root)): p.read_bytes()
            for p in sorted(self.root.rglob("*")) if p.is_file()
        }

    def resolves(self, md_rel: str, target: str) -> bool:
        """True when a relative link in md_rel resolves to an existing file."""
        base = posixpath.dirname(md_rel)
        path = target.split("#", 1)[0]
        return (self.root / posixpath.normpath(posixpath.join(base, path))).is_file()


class SharedRulesTest(unittest.TestCase):
    def test_rules_are_imported_not_copied(self) -> None:
        self.assertIs(pma.ROW_RE, plans_links.ROW_RE)
        self.assertIs(pma.CLOSED_RE, plans_links.CLOSED_RE)
        self.assertIs(pma.VETO_RE, plans_links.VETO_RE)
        self.assertIs(pma.RULE_FILE_RE, plans_links.RULE_FILE_RE)
        self.assertIs(pma.read_text, plans_links.read_text)


class ReportsTest(ArchiveCase):
    def test_old_unreferenced_report_is_archived_by_month(self) -> None:
        self.put(f"docs/plans/reports/{OLD}-a.md")
        counts = self.counts()
        self.assertEqual(counts["reports archived"], 1)
        self.assertEqual(counts["reports kept"], 0)
        self.assertTrue(self.exists(f"docs/plans/reports/archive/2026-10/{OLD}-a.md"))
        self.assertFalse(self.exists(f"docs/plans/reports/{OLD}-a.md"))

    def test_open_row_keeps_and_closed_row_archives_with_registry_link_rewrite(self) -> None:
        self.put(f"docs/plans/reports/{OLD}-closed.md")
        self.put(f"docs/plans/reports/{OLD}-open.md")
        self.put(
            "docs/plans/README.md",
            "| # | Item | Status |\n|---|---|---|\n"
            f"| 1 | [BL1](reports/{OLD}-closed.md) finished work | **done**, shipped in 1.0 |\n"
            f"| 2 | [BL2](reports/{OLD}-open.md) open work | needs_review |\n",
        )
        counts = self.counts()
        self.assertEqual(counts["reports archived"], 1)
        self.assertEqual(counts["reports kept"], 1)
        self.assertEqual(counts["links rewritten"], 1)
        self.assertTrue(self.exists(f"docs/plans/reports/{OLD}-open.md"))
        registry = self.read("docs/plans/README.md")
        self.assertIn(f"](reports/archive/2026-10/{OLD}-closed.md)", registry)
        self.assertIn(f"](reports/{OLD}-open.md)", registry)

    def test_closed_row_that_is_vetoed_still_keeps_the_report(self) -> None:
        self.put(f"docs/plans/reports/{OLD}-veto.md")
        self.put(
            "docs/plans/README.md",
            f"| 1 | [BL1](reports/{OLD}-veto.md) thing | not done yet, needs work |\n",
        )
        counts = self.counts()
        self.assertEqual(counts["reports kept"], 1)
        self.assertEqual(counts["reports archived"], 0)

    def test_keep_sources_scripts_rules_and_docs(self) -> None:
        self.put(f"docs/plans/reports/{OLD}-scripted.md")
        self.put(f"docs/plans/reports/{OLD}-rules.md")
        self.put(f"docs/plans/reports/{OLD}-docref.md")
        self.put(f"docs/plans/reports/{OLD}-notkept.md")
        self.put("scripts/check.sh", f"echo {OLD}-scripted.md\n")
        self.put("CONTEXT.md", f"see {OLD}-rules.md\n")
        self.put("docs/guide.md", f"see docs/plans/reports/{OLD}-docref.md\n")
        # a plan folder file is not a keep source
        self.put(f"docs/plans/{OLD}-note.md", f"mentions {OLD}-notkept.md\n")
        counts = self.counts()
        self.assertEqual(counts["reports kept"], 3)
        self.assertEqual(counts["reports archived"], 1)
        self.assertTrue(self.exists(f"docs/plans/reports/archive/2026-10/{OLD}-notkept.md"))

    def test_readme_and_archive_subfolder_are_out_of_scope(self) -> None:
        readme = self.put("docs/plans/reports/README.md", "# index\n")
        old_archived = self.put("docs/plans/reports/archive/2026-09/old.md")
        self.put(f"docs/plans/reports/{OLD}-real.md")
        counts = self.counts()
        self.assertEqual(counts["reports archived"], 1)
        self.assertEqual(counts["reports kept"], 0)
        self.assertTrue(readme.is_file())
        self.assertTrue(old_archived.is_file())

    def test_undated_file_uses_modification_time(self) -> None:
        mtime = dt.datetime(2026, 8, 15, 12, 0).timestamp()
        self.put("docs/plans/reports/notes.md", mtime=mtime)
        counts = self.counts()
        self.assertEqual(counts["reports archived"], 1)
        self.assertTrue(self.exists("docs/plans/reports/archive/2026-08/notes.md"))

    def test_young_file_is_kept_until_days_threshold(self) -> None:
        self.put(f"docs/plans/reports/{YOUNG}-fresh.md")
        self.assertEqual(self.counts()["reports kept"], 1)
        self.assertEqual(self.counts("--days", "0")["reports archived"], 1)
        self.assertTrue(self.exists(f"docs/plans/reports/archive/{TODAY:%Y-%m}/{YOUNG}-fresh.md"))

    def test_missing_plans_folder_prints_zero_counts(self) -> None:
        counts = self.counts()
        self.assertEqual({k: counts[k] for k in COUNT_KEYS}, {k: 0 for k in COUNT_KEYS})


class GitDateTest(ArchiveCase):
    def test_undated_tracked_file_uses_last_commit_date(self) -> None:
        self.put("docs/plans/reports/tracked-notes.md")
        env = dict(os.environ, GIT_COMMITTER_DATE="2026-07-03T10:00:00+00:00",
                   GIT_AUTHOR_DATE="2026-07-03T10:00:00+00:00",
                   GIT_AUTHOR_NAME="test", GIT_AUTHOR_EMAIL="test@example.invalid",
                   GIT_COMMITTER_NAME="test", GIT_COMMITTER_EMAIL="test@example.invalid")
        git = ["git", "-c", "user.name=test", "-c", "user.email=test@example.invalid"]
        subprocess.run(["git", "init", "-q"], cwd=self.root, check=True)
        subprocess.run(git + ["add", "."], cwd=self.root, check=True)
        subprocess.run(git + ["commit", "-q", "-m", "seed"], cwd=self.root, env=env, check=True)
        # mtime is now (young); the committer date must win over it
        counts = self.counts()
        self.assertEqual(counts["reports archived"], 1)
        self.assertTrue(self.exists("docs/plans/reports/archive/2026-07/tracked-notes.md"))
        ls = subprocess.run(["git", "ls-files"], cwd=self.root, capture_output=True,
                            text=True, check=True).stdout
        self.assertIn("docs/plans/reports/archive/2026-07/tracked-notes.md", ls)


class ParityIssueTest(ArchiveCase):
    def test_filed_moves_unfiled_is_unclassified_young_is_kept(self) -> None:
        self.put(f"docs/plans/parity-issues/{OLD}-filed.md",
                 "# x\n\n## Filed issue\n\n- Issue: [dmz006/datawatch-app#12]"
                 "(https://github.com/dmz006/datawatch-app/issues/12), open.\n")
        self.put(f"docs/plans/parity-issues/{OLD}-numbered.md",
                 "# x\n\n## Filed issue\n\n- Issue number: 55\n")
        self.put(f"docs/plans/parity-issues/{OLD}-draft.md", "# draft\n\nbody\n")
        self.put(f"docs/plans/parity-issues/{OLD}-notyet.md",
                 "# n\n\n## Filed issue\n\nNot yet filed.\n")
        self.put(f"docs/plans/parity-issues/{OLD}-needed.md",
                 "# n\n\n## Filed issue\n\nNot needed for this release.\n")
        self.put(f"docs/plans/parity-issues/{YOUNG}-young.md",
                 "## Filed issue\n\n- Issue: #77\n")
        counts = self.counts()
        self.assertEqual(counts["parity issues archived"], 2)
        self.assertEqual(counts["parity issues kept"], 1)
        self.assertEqual(counts["unclassified"], 3)
        self.assertTrue(self.exists(f"docs/plans/parity-issues/archive/2026-10/{OLD}-filed.md"))
        self.assertTrue(self.exists(f"docs/plans/parity-issues/archive/2026-10/{OLD}-numbered.md"))
        for name in ("draft", "notyet", "needed"):
            self.assertTrue(self.exists(f"docs/plans/parity-issues/{OLD}-{name}.md"))

    def test_list_unclassified_prints_names_only(self) -> None:
        self.put(f"docs/plans/parity-issues/{OLD}-draft.md", "# draft\n")
        self.put(f"docs/plans/parity-issues/{OLD}-filed.md",
                 "## Filed issue\n\n- Issue: #9\n")
        before = self.snapshot()
        result = self.tool("--list-unclassified")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.splitlines(), [f"{OLD}-draft.md"])
        self.assertEqual(self.snapshot(), before)

    def test_referenced_filed_draft_is_kept(self) -> None:
        self.put(f"docs/plans/parity-issues/{OLD}-filed.md",
                 "## Filed issue\n\n- Issue: #9\n")
        self.put("docs/plans/README.md",
                 f"| 1 | [P](parity-issues/{OLD}-filed.md) thing | needs_review |\n")
        counts = self.counts()
        self.assertEqual(counts["parity issues kept"], 1)
        self.assertEqual(counts["parity issues archived"], 0)
        self.assertEqual(counts["unclassified"], 0)


class LinkRewriteTest(ArchiveCase):
    def build(self) -> None:
        self.put("AGENT.md", "# agent\n")
        self.put(
            f"docs/plans/reports/{OLD}-moved.md",
            "See [agent](../../../AGENT.md), [kept](" + OLD + "-kept.md#part), "
            "and [self](" + OLD + "-moved.md).\n",
        )
        self.put(
            f"docs/plans/reports/{OLD}-kept.md",
            f"[moved](" + OLD + "-moved.md) and docs/plans/reports/" + OLD + "-moved.md\n",
        )
        self.put(
            "docs/plans/README.md",
            f"| 1 | [BL1](reports/{OLD}-kept.md) work | needs_review |\n"
            f"| 2 | [BL2](reports/{OLD}-moved.md) old | **done**, shipped |\n",
        )
        self.put(
            "docs/plans/2026-10-02-plan.md",
            f"[m](reports/{OLD}-moved.md) and plans/reports/{OLD}-moved.md\n",
        )

    def test_inbound_links_rewritten_and_moved_file_rebased(self) -> None:
        self.build()
        counts = self.counts()
        self.assertEqual(counts["reports archived"], 1)
        moved = f"docs/plans/reports/archive/2026-10/{OLD}-moved.md"
        self.assertTrue(self.exists(moved))

        # the moved file's own links still resolve from its deeper folder
        body = self.read(moved)
        self.assertIn("../../../../../AGENT.md", body)
        self.assertIn(f"[kept]({'../../' + OLD}-kept.md#part)", body)
        self.assertIn(f"[self]({OLD}-moved.md)", body)
        for target in ("../../../../../AGENT.md", "../../" + OLD + "-kept.md#part"):
            self.assertTrue(self.resolves(moved, target), target)

        # the kept report and the plan folder point at the archive path
        kept = self.read(f"docs/plans/reports/{OLD}-kept.md")
        self.assertIn(f"[moved](archive/2026-10/{OLD}-moved.md)", kept)
        self.assertIn(f"docs/plans/reports/archive/2026-10/{OLD}-moved.md", kept)
        self.assertTrue(self.resolves(f"docs/plans/reports/{OLD}-kept.md",
                                      f"archive/2026-10/{OLD}-moved.md"))
        plan = self.read("docs/plans/2026-10-02-plan.md")
        self.assertIn(f"[m](reports/archive/2026-10/{OLD}-moved.md)", plan)
        self.assertIn(f"plans/reports/archive/2026-10/{OLD}-moved.md", plan)

        # registry: the open row to the kept report is untouched; the closed
        # row to the moved file now points at the archive
        registry = self.read("docs/plans/README.md")
        self.assertIn(f"[BL1](reports/{OLD}-kept.md)", registry)
        self.assertIn(f"[BL2](reports/archive/2026-10/{OLD}-moved.md)", registry)

        # 1 registry + 2 kept report + 1 plan link ... every rewrite is counted
        self.assertGreaterEqual(counts["links rewritten"], 6)


class RunModeTest(ArchiveCase):
    def build_mixed(self) -> None:
        self.put(f"docs/plans/reports/{OLD}-a.md")
        self.put(f"docs/plans/reports/{OLD}-b.md", f"[a](" + OLD + "-a.md)\n")
        self.put(f"docs/plans/parity-issues/{OLD}-filed.md", "## Filed issue\n\n- Issue: #3\n")
        self.put(f"docs/plans/parity-issues/{OLD}-draft.md", "# d\n")
        self.put(
            "docs/plans/README.md",
            f"| 1 | [BL1](reports/{OLD}-b.md) open | needs_review |\n"
            f"| 2 | [BL2](reports/{OLD}-a.md) closed | **done**, shipped |\n",
        )

    def test_dry_run_changes_nothing_and_matches_real_counts(self) -> None:
        self.build_mixed()
        before = self.snapshot()
        dry = self.counts("--dry-run")
        self.assertEqual(self.snapshot(), before)
        real = self.counts()
        self.assertEqual(dry, real)

    def test_second_run_changes_nothing(self) -> None:
        self.build_mixed()
        first = self.counts()
        self.assertGreater(first["reports archived"], 0)
        after_first = self.snapshot()
        second = self.counts()
        self.assertEqual(self.snapshot(), after_first)
        self.assertEqual(second["reports archived"], 0)
        self.assertEqual(second["parity issues archived"], 0)
        self.assertEqual(second["links rewritten"], 0)
        self.assertEqual(second["unclassified"], first["unclassified"])

    def test_normal_output_is_counts_only(self) -> None:
        self.build_mixed()
        result = self.tool()
        self.assertEqual(result.returncode, 0, result.stderr)
        lines = result.stdout.splitlines()
        self.assertEqual([line.split(": ")[0] for line in lines], list(COUNT_KEYS))
        self.assertNotIn(".md", result.stdout)

    def test_list_unclassified_is_read_only(self) -> None:
        self.build_mixed()
        before = self.snapshot()
        result = self.tool("--list-unclassified")
        self.assertEqual(result.stdout.splitlines(), [f"{OLD}-draft.md"])
        self.assertEqual(self.snapshot(), before)


if __name__ == "__main__":
    unittest.main(verbosity=2)
