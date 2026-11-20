#!/usr/bin/env python3
"""Offline unit tests for the worktree check in scripts/supervisor-watch.py (unittest only).

Run from the repository root:
    python3 scripts/tests/test_supervisor_watch_worktree.py

Fixtures live in a tempfile.TemporaryDirectory that the module's WT constant is patched to.
No network, git or tmux is used.
"""

import importlib.util
import os
import tempfile
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
MODULE_PATH = os.path.join(os.path.dirname(HERE), "supervisor-watch.py")
PRD = "840f0001"


def load_module():
    spec = importlib.util.spec_from_file_location("supervisor_watch", MODULE_PATH)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


sw = load_module()


class WorktreeCheckTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.wt = os.path.join(self.tmp.name, "prd-worktrees")
        os.makedirs(os.path.join(self.wt, PRD))
        patcher = mock.patch.object(sw, "WT", self.wt)
        patcher.start()
        self.addCleanup(patcher.stop)

    def test_other_repository_without_go_mod_no_finding(self):
        other = os.path.join(self.tmp.name, "other-repo")
        os.makedirs(other)
        self.assertFalse(sw.is_datawatch_worktree(other, PRD))
        self.assertIsNone(sw.worktree_finding(other, PRD))

    def test_worktree_without_go_mod_gives_finding(self):
        d = os.path.join(self.wt, PRD)
        self.assertTrue(sw.is_datawatch_worktree(d + "/", PRD))
        self.assertIn("has no go.mod", sw.worktree_finding(d, PRD))

    def test_worktree_with_go_mod_no_finding(self):
        d = os.path.join(self.wt, PRD)
        open(os.path.join(d, "go.mod"), "w").close()
        self.assertIsNone(sw.worktree_finding(d, PRD))

    def test_missing_project_dir_no_finding(self):
        for v in (None, ""):
            self.assertFalse(sw.is_datawatch_worktree(v, PRD))
            self.assertIsNone(sw.worktree_finding(v, PRD))


if __name__ == "__main__":
    unittest.main()
