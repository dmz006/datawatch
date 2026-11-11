#!/usr/bin/env python3
"""Tests for dependency-update-summary.py. Run: python3 scripts/dependency-update-summary_test.py"""
import importlib.util
import json
import os
import subprocess
import sys
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "dependency-update-summary.py")
_spec = importlib.util.spec_from_file_location("summary_mod", SCRIPT)
mod = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mod)


def dep(name, kind="patch", result="passed", **kw):
    d = {"name": name, "ecosystem": "gomod", "old": "1.0.0", "new": "1.0.1", "kind": kind, "result": result}
    d.update(kw)
    return d


def rows(out):
    return [l for l in out.splitlines() if l.startswith("| ") and not l.startswith("| Dependency") and "---" not in l]


class SummaryTest(unittest.TestCase):
    def test_ordering(self):
        out = mod.render([dep("z", "patch"), dep("b", "minor"), dep("a", "patch"),
                          dep("m", "major"), dep("s", "security")])
        names = [r.split("|")[1].strip() for r in rows(out)]
        self.assertEqual(names, ["s", "m", "b", "a", "z"])

    def test_empty_silent(self):
        self.assertEqual(mod.render([]), "")
        p = subprocess.run([sys.executable, SCRIPT], input="[]", capture_output=True, text=True)
        self.assertEqual((p.returncode, p.stdout), (0, ""))

    def test_header_counts_and_howto(self):
        out = mod.render([dep("a"), dep("b", result="dropped"), dep("c", result="skipped"),
                          dep("d", result="failed")])
        self.assertIn("updated 1 dependency, dropped 1 and skipped 1", out)
        self.assertIn("docs/howto/automation-dependency-update.md", out)
        self.assertIn("| Dependency | Old version | New version | Kind | Test result |", out)

    def test_pipe_escaping(self):
        out = mod.render([dep("a|b", "major", note="use x | y")])
        self.assertIn("a\\|b", out)
        self.assertIn("use x \\| y", out)

    def test_trimming_to_last_40_lines(self):
        note = "\n".join("line%d" % n for n in range(100))
        out = mod.render([dep("a", result="failed", note=note)])
        self.assertIn("line99", out)
        self.assertIn("line60", out)
        self.assertNotIn("line59\n", out)

    def test_attention_section(self):
        out = mod.render([dep("ok"), dep("bad", result="failed", note="boom"),
                          dep("gone", result="dropped", note="conflict"),
                          dep("big", "major", note="read the migration guide")])
        att = out.split("## Needs your attention")[1]
        for s in ("bad", "boom", "gone", "conflict", "big", "read the migration guide"):
            self.assertIn(s, att)
        self.assertNotIn("**ok", att)

    def test_no_attention_section_when_clean(self):
        self.assertNotIn("Needs your attention", mod.render([dep("a"), dep("b", "minor")]))

    def test_bad_input_exits_nonzero(self):
        p = subprocess.run([sys.executable, SCRIPT], input="{", capture_output=True, text=True)
        self.assertNotEqual(p.returncode, 0)


if __name__ == "__main__":
    unittest.main()
