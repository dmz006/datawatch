#!/usr/bin/env python3
"""Tests for dependency-update-classify.py. Run: python3 scripts/dependency-update-classify_test.py"""
import importlib.util
import json
import os
import subprocess
import sys
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "dependency-update-classify.py")
_spec = importlib.util.spec_from_file_location("classify_mod", SCRIPT)
mod = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mod)

# (description, ecosystem, name, old, new, security, expected)
CASES = [
    ("security flag wins", "gomod", "example.com/a", "v1.0.0", "v1.0.1", True, "security"),
    ("security beats major", "docker", "alpine", "1.0", "2.0", True, "security"),
    ("go patch", "gomod", "example.com/a", "v1.2.3", "v1.2.4", None, "patch"),
    ("go minor", "gomod", "example.com/a", "v1.2.3", "v1.3.0", None, "minor"),
    ("go major", "gomod", "example.com/a", "v1.2.3", "v2.0.0", None, "major"),
    ("go v0 minor is major", "gomod", "example.com/a", "v0.4.1", "v0.5.0", None, "major"),
    ("go v0 patch is patch", "gomod", "example.com/a", "v0.4.1", "v0.4.2", None, "patch"),
    ("go prerelease ignored", "gomod", "example.com/a", "v1.2.3-rc.1", "v1.2.3", None, "patch"),
    ("go prerelease minor", "gomod", "example.com/a", "v1.2.3", "v1.3.0-beta.2", None, "minor"),
    ("go +incompatible major", "gomod", "example.com/a", "v2.0.0+incompatible", "v3.0.0+incompatible", None, "major"),
    ("go +incompatible patch", "gomod", "example.com/a", "v2.0.0+incompatible", "v2.0.1+incompatible", None, "patch"),
    ("go path /v2 to /v3", "gomod", "example.com/a", "example.com/a/v2@v2.5.0", "example.com/a/v3@v3.0.0", None, "major"),
    ("go path major differs, same version", "gomod", "example.com/a", "example.com/a@v1.0.0", "example.com/a/v2@v1.0.0", None, "major"),
    ("go unparseable", "gomod", "example.com/a", "master", "v1.0.0", None, "unknown"),
    ("docker minor", "docker", "golang", "1.22", "1.23", None, "minor"),
    ("docker patch with suffix", "docker", "golang", "1.22.3-alpine", "1.22.4-alpine", None, "patch"),
    ("docker major", "docker", "node", "20-bookworm", "22-bookworm", None, "major"),
    ("docker distro suffix change same prefix", "docker", "golang", "1.22.3-alpine", "1.22.3-bookworm", None, "patch"),
    ("docker digest only both sides", "docker", "alpine", "sha256:aaa", "sha256:bbb", None, "patch"),
    ("docker non-numeric both", "docker", "alpine", "latest", "stable", None, "patch"),
    ("docker non-numeric to numeric", "docker", "alpine", "latest", "3.19", None, "unknown"),
    ("docker numeric with digest", "docker", "alpine", "3.18@sha256:aaa", "3.19@sha256:bbb", None, "minor"),
    ("action major", "github-actions", "actions/checkout", "v4", "v5", None, "major"),
    ("action minor", "github-actions", "actions/checkout", "v4.1.2", "v4.2.0", None, "minor"),
    ("action patch", "github-actions", "actions/checkout", "v4.1.2", "v4.1.3", None, "patch"),
    ("action v4 to v4.1.2 is minor", "github-actions", "actions/checkout", "v4", "v4.1.2", None, "minor"),
    ("action comment version", "github-actions", "actions/checkout", "a" * 40 + " # v4.1.2", "b" * 40 + " # v4.1.3", None, "patch"),
    ("action comment major", "github-actions", "actions/checkout", "a" * 40 + " # v4.1.2", "b" * 40 + " # v5.0.0", None, "major"),
    ("action bare hash unknown", "github-actions", "actions/checkout", "a" * 40, "b" * 40, None, "unknown"),
    ("security false falls through", "gomod", "example.com/a", "v1.0.0", "v1.1.0", False, "minor"),
]


def _item(eco, name, old, new, sec):
    d = {"name": name, "ecosystem": eco, "old": old, "new": new}
    if sec is not None:
        d["security"] = sec
    return d


class ClassifyTable(unittest.TestCase):
    def test_table(self):
        self.assertGreaterEqual(len(CASES), 20)
        for desc, eco, name, old, new, sec, want in CASES:
            with self.subTest(desc):
                got = mod.process([_item(eco, name, old, new, sec)])[0]
                self.assertEqual(got["kind"], want)
                self.assertEqual(got["name"], name)


def run_cli(text):
    return subprocess.run([sys.executable, SCRIPT], input=text, capture_output=True, text=True)


class CLI(unittest.TestCase):
    def test_round_trip(self):
        items = [_item("gomod", "x/y", "v1.0.0", "v1.0.1", None)]
        r = run_cli(json.dumps(items))
        self.assertEqual(r.returncode, 0)
        out = json.loads(r.stdout)
        self.assertEqual(out[0]["kind"], "patch")

    def test_empty_list(self):
        r = run_cli("[]")
        self.assertEqual(r.returncode, 0)
        self.assertEqual(json.loads(r.stdout), [])

    def test_malformed(self):
        bad = [
            "not json",
            "{}",
            "[1]",
            json.dumps([{"name": "a", "ecosystem": "npm", "old": "1", "new": "2"}]),
            json.dumps([{"name": "a", "ecosystem": "gomod", "old": "v1.0.0"}]),
            json.dumps([{"name": "a", "ecosystem": "gomod", "old": "v1", "new": "v2", "security": "yes"}]),
        ]
        for text in bad:
            with self.subTest(text):
                r = run_cli(text)
                self.assertNotEqual(r.returncode, 0)
                self.assertIn("malformed input", r.stderr)


if __name__ == "__main__":
    unittest.main()
