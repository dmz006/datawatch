#!/usr/bin/env python3
"""Offline unit tests for merge_shadow_lines in scripts/supervisor-watch.py (unittest only).

Run from the repository root:
    env -u TMUX -u TMUX_TMPDIR python3 -I -m unittest discover -s scripts/tests -p "test_supervisor_watch_merge_shadow.py" -v

get_json is patched, so no network is used and only the REST read endpoint is named.
"""

import importlib.util
import os
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
MODULE_PATH = os.path.join(os.path.dirname(HERE), "supervisor-watch.py")


def load_module():
    spec = importlib.util.spec_from_file_location("supervisor_watch_under_test", MODULE_PATH)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def counts(pm, pd, mm, md, nm=0, nd=0):
    return {"patch": {"matched": pm, "different": pd}, "minor": {"matched": mm, "different": md},
            "major": {"matched": 0, "different": 0}, "none": {"matched": nm, "different": nd}}


class MergeShadowLinesTest(unittest.TestCase):
    def setUp(self):
        self.mod = load_module()

    def test_sums_sizes_across_projects_and_skips_empty_sizes(self):
        payload = {"projects": [
            {"project": "a", "counts": counts(2, 1, 0, 0)},
            {"project": "b", "counts": counts(1, 0, 3, 2)},
        ]}
        with mock.patch.object(self.mod, "get_json", return_value=payload) as gj:
            lines = self.mod.merge_shadow_lines()
        self.assertEqual(lines, ["MERGE SHADOW minor: matched 3 different 2",
                                 "MERGE SHADOW patch: matched 3 different 1"])
        gj.assert_called_once()
        self.assertEqual(gj.call_args[0][0], "https://localhost:8443/api/autonomous/merge/shadow")

    def test_none_when_no_comparisons(self):
        payload = {"projects": [{"project": "a", "counts": counts(0, 0, 0, 0)}]}
        with mock.patch.object(self.mod, "get_json", return_value=payload):
            self.assertEqual(self.mod.merge_shadow_lines(), [])
        with mock.patch.object(self.mod, "get_json", return_value={"projects": []}):
            self.assertEqual(self.mod.merge_shadow_lines(), [])

    def test_none_when_endpoint_unreachable(self):
        with mock.patch.object(self.mod, "get_json", return_value=None):
            self.assertEqual(self.mod.merge_shadow_lines(), [])


if __name__ == "__main__":
    unittest.main()
