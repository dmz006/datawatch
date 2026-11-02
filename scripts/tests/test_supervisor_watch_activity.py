#!/usr/bin/env python3
"""Offline unit tests for the activity measure in scripts/supervisor-watch.py (unittest only).

Run from the repository root:
    env -u TMUX -u TMUX_TMPDIR python3 -I scripts/tests/test_supervisor_watch_activity.py -v
    env -u TMUX -u TMUX_TMPDIR python3 -I -m unittest discover -s scripts/tests -p "test_supervisor_watch_activity.py"

Hermeticity: every fixture lives in a tempfile.TemporaryDirectory: a fake worktree tree (the module's
WT constant is patched to it), a fake pi transcript base (PI_CODING_AGENT_DIR patched through
mock.patch.dict on os.environ) and *.jsonl transcripts whose mtimes are set with os.utime. HOME is
pointed at the temporary directory too so the module's ~ lookups cannot see real data, and
task_worktrees is patched where a task worktree is needed so no git or tmux process is started.
No network is used.
"""

import importlib.util
import os
import tempfile
import time
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
MODULE_PATH = os.path.join(os.path.dirname(HERE), "supervisor-watch.py")

PRD = "840f0001"
MIN = 60


def load_module():
    spec = importlib.util.spec_from_file_location("supervisor_watch", MODULE_PATH)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


sw = load_module()


def touch(path, age_seconds, now):
    """Create the file (and parents) and set its mtime to age_seconds before now."""
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as fh:
        fh.write("{}\n")
    t = now - age_seconds
    os.utime(path, (t, t))


class ActivityTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        root = os.path.realpath(self.tmp.name)
        self.wt = os.path.join(root, "prd-worktrees")
        self.pi = os.path.join(root, "pi-agent")
        self.home = os.path.join(root, "home")
        os.makedirs(self.wt)
        os.makedirs(self.pi)
        os.makedirs(self.home)
        self.now = time.time()
        env = mock.patch.dict(os.environ, {"PI_CODING_AGENT_DIR": self.pi, "HOME": self.home})
        env.start()
        self.addCleanup(env.stop)
        wt = mock.patch.object(sw, "WT", self.wt)
        wt.start()
        self.addCleanup(wt.stop)
        self.tasks = []
        tw = mock.patch.object(sw, "task_worktrees", lambda prd_id: list(self.tasks))
        tw.start()
        self.addCleanup(tw.stop)

    def worktree_file(self, age_min):
        touch(os.path.join(self.wt, PRD, "src", "file.txt"), age_min * MIN, self.now)

    def transcript(self, worktree, age_min, name="s.jsonl"):
        touch(os.path.join(sw.pi_transcript_dir(worktree), name), age_min * MIN, self.now)

    def measure(self):
        return sw.newest_activity_measure(PRD)

    def test_silent_threshold_is_60(self):
        self.assertEqual(sw.SILENT_MIN, 60)

    def test_recent_transcript_old_worktree_not_silent(self):
        self.worktree_file(120)
        self.transcript(os.path.join(self.wt, PRD), 3)
        age, label = self.measure()
        self.assertEqual(label, "transcript")
        self.assertLess(age, sw.SILENT_MIN * MIN)
        self.assertLess(sw.newest_activity(PRD), sw.SILENT_MIN * MIN)

    def test_old_transcript_old_worktree_silent(self):
        self.worktree_file(70)
        self.transcript(os.path.join(self.wt, PRD), 120)
        age, label = self.measure()
        self.assertGreater(age, sw.SILENT_MIN * MIN)
        self.assertNotEqual(label, "transcript")
        self.assertEqual(label, "worktree file")

    def test_missing_transcript_folder_uses_worktree_measure(self):
        self.worktree_file(75)
        self.assertFalse(os.path.exists(sw.pi_transcript_dir(os.path.join(self.wt, PRD))))
        age, label = self.measure()
        self.assertEqual(label, "worktree file")
        self.assertAlmostEqual(age, 75 * MIN, delta=30)

    def test_task_worktree_transcript_is_newest(self):
        task = os.path.join(self.wt, PRD + "-tasks", "some-task")
        os.makedirs(task)
        self.tasks = [task]
        self.worktree_file(120)
        self.transcript(os.path.join(self.wt, PRD), 100)
        self.transcript(task, 2)
        age, label = self.measure()
        self.assertEqual(label, "transcript")
        self.assertAlmostEqual(age, 2 * MIN, delta=30)

    def test_nothing_anywhere_returns_none(self):
        self.assertEqual(self.measure(), (None, None))

    def test_newest_of_several_transcripts_wins(self):
        self.worktree_file(120)
        d = os.path.join(self.wt, PRD)
        self.transcript(d, 80, "old.jsonl")
        self.transcript(d, 10, "new.jsonl")
        age, _ = self.measure()
        self.assertAlmostEqual(age, 10 * MIN, delta=30)


if __name__ == "__main__":
    unittest.main()
