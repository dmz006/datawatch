#!/usr/bin/env python3
"""Tests for scripts/branch-audit.py (the thin daemon unlanded-work reader).

Every case writes a fake `datawatch` shell script that prints a canned
unlanded-work JSON body, points the real script at it with the DATAWATCH_BIN
override, and asserts the printed rows, the summary line and the exit status
for both zero and non-zero unlanded.

Run:  python3 tests/test_branch_audit.py
"""
from __future__ import annotations

import json
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
SCRIPT = REPO / "scripts" / "branch-audit.py"

ZERO_JSON = {
    "branches": [
        {
            "automaton_id": "a1",
            "branch": "automaton/a1",
            "title": "Docs refresh",
            "commits": 0,
            "last_activity": "2026-11-01T09:00:00Z",
            "recommendation": "landed",
            "reason": "every commit is already on main by content",
        },
        {
            "automaton_id": "a2",
            "branch": "automaton/a2",
            "title": "Old experiment",
            "commits": 3,
            "last_activity": "2026-10-02T08:00:00Z",
            "recommendation": "superseded",
            "reason": "every file it touched changed on main since the fork",
        },
    ]
}

NONZERO_JSON = {
    "branches": [
        {
            "automaton_id": "b1",
            "branch": "automaton/b1",
            "title": "Automatic lane fill",
            "commits": 12,
            "last_activity": "2026-10-19T14:30:00Z",
            "recommendation": "needs_decision",
            "reason": "commits not on main, nothing shows they were replaced",
        },
        {
            "automaton_id": "b2",
            "branch": "automaton/b2",
            "title": "Live-ish",
            "commits": 2,
            "last_activity": "2026-11-02T00:00:00Z",
            "recommendation": "",
            "reason": "",
        },
        {
            "automaton_id": "b3",
            "branch": "automaton/b3",
            "title": "Docs refresh",
            "commits": 0,
            "last_activity": "2026-11-01T09:00:00Z",
            "recommendation": "landed",
            "reason": "",
        },
    ]
}


class BranchAuditCase(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp(prefix="branch-audit-"))
        self.addCleanup(lambda: self.cleanup())
        self.fake = self.tmp / "datawatch"
        self._fake_body = ""

    def cleanup(self) -> None:
        import shutil

        shutil.rmtree(self.tmp, ignore_errors=True)

    def _write_fake(self, body: str, exit_code: int = 0) -> None:
        self.fake.write_text(
            "#!/bin/sh\n"
            f"cat <<'DWJSON'\n{body}\nDWJSON\n"
            f"exit {exit_code}\n"
        )
        self.fake.chmod(self.fake.stat().st_mode | stat.S_IXUSR)

    def run_script(self) -> subprocess.CompletedProcess:
        env = dict(os.environ, DATAWATCH_BIN=str(self.fake))
        return subprocess.run(
            [sys.executable, str(SCRIPT)],
            capture_output=True,
            text=True,
            env=env,
        )

    def test_zero_unlanded_exits_zero(self) -> None:
        self._write_fake(json.dumps(ZERO_JSON))
        p = self.run_script()
        self.assertEqual(p.returncode, 0, p.stdout + p.stderr)
        self.assertNotIn("FINDING:", p.stdout)
        self.assertIn("summary: 0 unlanded (finding), 1 pending, 0 live, 1 landed", p.stdout)
        self.assertIn("LANDED   a1       landed", p.stdout)
        self.assertIn("PENDING  a2       superseded", p.stdout)
        self.assertIn("ahead=0   last=11-01 09:00 UTC Docs refresh (automaton/a1)", p.stdout)
        self.assertIn("ahead=3   last=10-02 08:00 UTC Old experiment (automaton/a2)", p.stdout)

    def test_nonzero_unlanded_exits_one_with_finding(self) -> None:
        self._write_fake(json.dumps(NONZERO_JSON))
        p = self.run_script()
        self.assertEqual(p.returncode, 1, p.stdout + p.stderr)
        self.assertIn("summary: 1 unlanded (finding), 0 pending, 1 live, 1 landed", p.stdout)
        self.assertIn(
            "FINDING: 1 archived branch(es) with work not on main; merge or record as dropped",
            p.stdout,
        )
        self.assertIn("UNLANDED b1       needs_decision", p.stdout)
        self.assertIn("LIVE     b2       -              ", p.stdout)
        self.assertIn("LANDED   b3       landed", p.stdout)
        self.assertIn("ahead=12  last=10-19 14:30 UTC Automatic lane fill (automaton/b1)", p.stdout)
        # least settled first
        lines = [l for l in p.stdout.splitlines() if l[:1] in "ULP" and ("UNLANDED" in l or "LIVE" in l or "LANDED" in l or "PENDING" in l)]
        self.assertEqual(lines[0].split()[0], "UNLANDED")

    def test_unparseable_body_exits_two(self) -> None:
        self._write_fake("daemon said no\n")
        p = self.run_script()
        self.assertEqual(p.returncode, 2, p.stdout + p.stderr)
        self.assertIn("reading the daemon unlanded-work list failed", p.stderr)

    def test_failing_cli_exits_two(self) -> None:
        self._write_fake("", exit_code=5)
        p = self.run_script()
        self.assertEqual(p.returncode, 2, p.stdout + p.stderr)

    def test_no_cli_exits_two(self) -> None:
        # empty PATH too: the daemon host has a real `datawatch` binary
        env = dict(os.environ, DATAWATCH_BIN="", PATH="")
        p = subprocess.run(
            [sys.executable, str(SCRIPT)],
            capture_output=True,
            text=True,
            env=env,
        )
        self.assertEqual(p.returncode, 2, p.stdout + p.stderr)
        self.assertIn("no datawatch CLI found", p.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
