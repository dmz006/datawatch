#!/usr/bin/env python3
"""Offline unit tests for scripts/dismissed_alerts_watch.py (unittest).

Run:
    python3 scripts/tests/test_dismissed_alerts_watch.py
(works from the repository root, or from any directory; the test file
computes the scripts/ directory from its own location, so no -I/-m
machinery is needed)

Hermeticity: importing dismissed_alerts_watch has no import-time side
effects -- it only imports json/os/subprocess/sys (and yaml, with a
sys.exit if PyYAML is absent); it opens no network connection and spawns
no process at import time, so importing it here is safe. Every test
replaces the `gh` binary with a fake shell script in a
tempfile.mkdtemp() directory prepended to PATH via
unittest.mock.patch.dict (stopped in tearDown, along with rmtree of the
dir); nothing outside that tempdir is written. The fake emits two pages
concatenated, exactly as unsliced `gh api --paginate` output is: there
is no `--slurp` folding to emulate, and the fake rejects `--slurp` with
the real gh's "unknown flag" error so a regression to that flag fails
the test immediately.

Covered:
  1. gh_api_paginated flattens both pages into one item list, error None.
  2. A non-zero gh exit returns (None, non-empty message).
  3. A page that is a dict with a "message" field returns (None, a
     string containing that message).
  4. The argv the fake receives includes --paginate and does NOT
     include the nonexistent --slurp flag (verified via the fake
     writing its argv to a file).
"""

import json
import os
import stat
import sys
import tempfile
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPTS_DIR = os.path.dirname(HERE)
if SCRIPTS_DIR not in sys.path:
    sys.path.insert(0, SCRIPTS_DIR)

import dismissed_alerts_watch as daw  # noqa: E402  (see hermeticity note)

# The two pages the fake gh emits. A dict per item, matching the shape
# the dismissed-alerts code reads (number / rule.id / dismissed_reason).
PAGE_1 = [{"number": 11, "rule": {"id": "rule-one"},
           "dismissed_reason": "accepted_risk"}]
PAGE_2 = [{"number": 22, "rule": {"id": "rule-two"},
           "dismissed_reason": "wont_fix"},
          {"number": 23, "rule": {"id": "rule-three"},
           "dismissed_reason": "false_positive"}]
EXPECTED_ITEMS = PAGE_1 + PAGE_2
API_ERROR_PAGE = {"message": "fake: secondary rate limit exceeded"}

FAKE_GH = r'''#!/bin/sh
# Fake `gh` for offline tests. FAKE_GH_ARGV_FILE (required) receives the
# full argv, one arg per line. FAKE_GH_MODE selects behavior:
#   pages     (default) two array pages, as below
#   api_error page 2 is a dict with a "message" field (API error page)
#   fail      non-zero exit with a stderr message
[ -n "$FAKE_GH_ARGV_FILE" ] || { echo "fake gh: FAKE_GH_ARGV_FILE unset" >&2; exit 64; }
printf '%s\n' "$@" > "$FAKE_GH_ARGV_FILE"
case "${FAKE_GH_MODE:-pages}" in
  fail)
    echo "fake gh: simulated non-zero exit" >&2
    exit 3
    ;;
  api_error)
    P1='__PAGE_1__'
    P2='__API_ERROR_PAGE__'
    ;;
  *)
    P1='__PAGE_1__'
    P2='__PAGE_2__'
    ;;
esac
for a in "$@"; do
    if [ "$a" = "--slurp" ]; then
        # Real gh (e.g. 2.46.0): "unknown flag: --slurp", exit nonzero.
        echo "unknown flag: --slurp" >&2
        exit 1
    fi
done
# unsliced `gh api --paginate`: the pages concatenated
printf '%s\n%s\n' "$P1" "$P2"
'''


class GhhApiPaginatedTest(unittest.TestCase):
    """drives daw.gh_api_paginated against the fake gh on PATH."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="test_dismissed_alerts_watch-")
        self.argv_file = os.path.join(self.tmp, "gh-argv.txt")

        fake = FAKE_GH.replace("__PAGE_1__", json.dumps(PAGE_1)) \
                     .replace("__PAGE_2__", json.dumps(PAGE_2)) \
                     .replace("__API_ERROR_PAGE__", json.dumps(API_ERROR_PAGE))
        self.fake_gh = os.path.join(self.tmp, "gh")
        with open(self.fake_gh, "w", encoding="utf-8") as f:
            f.write(fake)
        os.chmod(self.fake_gh, os.stat(self.fake_gh).st_mode
                 | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

        self._env_patch = mock.patch.dict(
            os.environ,
            {"PATH": self.tmp + os.pathsep + os.environ.get("PATH", ""),
             "FAKE_GH_ARGV_FILE": self.argv_file,
             "FAKE_GH_MODE": "pages"},
        )
        self._env_patch.start()
        self.addCleanup(self._env_patch.stop)
        self.addCleanup(lambda: __import__("shutil").rmtree(self.tmp))

    def gh(self, mode="pages"):
        """Call gh_api_paginated with FAKE_GH_MODE=mode for that one
        call, restoring the mode afterwards."""
        saved = os.environ["FAKE_GH_MODE"]
        os.environ["FAKE_GH_MODE"] = mode
        try:
            return daw.gh_api_paginated("owner/repo",
                                        "code-scanning/alerts", None)
        finally:
            os.environ["FAKE_GH_MODE"] = saved

    # (1) slurp output: both pages flattened, no error
    def test_flattens_items_across_both_pages(self):
        items, err = self.gh()
        self.assertIsNone(err)
        self.assertEqual(items, EXPECTED_ITEMS)
        self.assertEqual(len(items), len(PAGE_1) + len(PAGE_2))
        self.assertEqual([i["number"] for i in items], [11, 22, 23])

    # (2) non-zero gh exit -> (None, non-empty message)
    def test_nonzero_exit_returns_error_message(self):
        items, err = self.gh("fail")
        self.assertIsNone(items)
        self.assertIsInstance(err, str)
        self.assertTrue(err)
        self.assertIn("simulated non-zero exit", err)

    # (3) a page that is a dict with a message field -> API error surfaced
    def test_dict_page_with_message_field_is_an_error(self):
        items, err = self.gh("api_error")
        self.assertIsNone(items)
        self.assertIsInstance(err, str)
        self.assertIn("fake: secondary rate limit exceeded", err)

    # (4) argv the fake received includes --paginate and NOT --slurp
    def test_argv_has_paginate_and_no_slurp(self):
        self.gh()
        with open(self.argv_file, encoding="utf-8") as f:
            argv = [line for line in f.read().splitlines() if line]
        self.assertIn("--paginate", argv)
        self.assertNotIn("--slurp", argv)
        # sanity: the endpoint the test asked for is on the command line
        self.assertIn("repos/owner/repo/code-scanning/alerts", argv)


if __name__ == "__main__":
    unittest.main(verbosity=2)
