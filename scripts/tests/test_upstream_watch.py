#!/usr/bin/env python3
"""Unit tests for scripts/upstream_watch.py (offline only, unittest).

Run from the repository root (any of these works; none touches the network):
    python3 -I scripts/tests/test_upstream_watch.py
    python3 -I -m unittest discover -s scripts/tests -p "test_upstream_watch.py"
    python3 -m unittest scripts/tests/test_upstream_watch.py

Note on `python3 -I -m unittest scripts/tests/test_upstream_watch.py`: that
exact combination cannot work under any test file. -I implies -P, which
removes the current directory from sys.path; unittest then converts the
file path to the dotted name `scripts.tests.test_upstream_watch`
(unittest.main._convert_name) and does `__import__("scripts")`
(TestLoader.loadTestsFromName) *before* this module's body executes, so no
sys.path manipulation inside the test file can help. Drop -I and the same
`-m unittest` command works (the repo root then resolves `scripts` as a
namespace package on the path).

Hermeticity: every test that drives a network-facing function replaces
upstream_watch.fetch with a FakeFeed (unittest.mock.patch.object); the
end-to-end CLI test goes through --offline-fixtures instead. All repository
trees are built inside a tempfile.TemporaryDirectory and nothing is written
into the real repo. The module's FIXTURES global is reset after the CLI
test.
"""

import contextlib
import io
import json
import os
import sys
import tempfile
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPTS_DIR = os.path.dirname(HERE)
if SCRIPTS_DIR not in sys.path:
    sys.path.insert(0, SCRIPTS_DIR)

import upstream_watch as up  # noqa: E402

# Feed URLs the script derives from owner/repo constants (kept explicit here
# so the fixtures map exactly at the URLs the code will request).
TRIVY_LATEST_URL = ("https://api.github.com/repos/"
                    "aquasecurity/trivy/releases/latest")


class FakeFeed:
    """Stand-in for upstream_watch.fetch: url -> body text or Exception to
    raise. A URL with no registered response raises FeedError, the same way
    a missing offline fixture does. Records every call for assertions."""

    def __init__(self, responses):
        self.responses = responses
        self.calls = []

    def __call__(self, url, method="GET", body=None):
        self.calls.append({"method": method, "url": url, "body": body})
        if url not in self.responses:
            raise up.FeedError(url, "no fake response registered for URL")
        value = self.responses[url]
        if isinstance(value, Exception):
            raise value
        return value


def patch_feed(feed):
    """mock.patch.upstream_watch.fetch bound to a FakeFeed instance."""
    return mock.patch.object(up, "fetch", new=feed)


def make_worktree(root, dockerfiles=None, workflows=None, dockerfile_dev=None):
    """Create a minimal repository tree under `root` (a tempdir path)."""
    if dockerfiles:
        d = os.path.join(root, "docker", "dockerfiles")
        os.makedirs(d, exist_ok=True)
        for name, text in dockerfiles.items():
            with open(os.path.join(d, name), "w", encoding="utf-8") as f:
                f.write(text)
    if workflows:
        wd = os.path.join(root, ".github", "workflows")
        os.makedirs(wd, exist_ok=True)
        for name, text in workflows.items():
            with open(os.path.join(wd, name), "w", encoding="utf-8") as f:
                f.write(text)
    if dockerfile_dev is not None:
        with open(os.path.join(root, "Dockerfile.dev"), "w", encoding="utf-8") as f:
            f.write(dockerfile_dev)


def make_trivy_version(root, text):
    """Write scripts/trivy-version.txt under `root`."""
    d = os.path.join(root, "scripts")
    os.makedirs(d, exist_ok=True)
    with open(os.path.join(d, "trivy-version.txt"), "w", encoding="utf-8") as f:
        f.write(text)


def run_check_pins(root, dockerfiles, feed):
    """Build `dockerfiles` under a tempdir and run up.check_pins with the
    fake feed. Returns (findings, unmapped, checked, not_checked)."""
    make_worktree(root, dockerfiles=dockerfiles)
    not_checked = []
    with patch_feed(feed):
        findings, unmapped, checked = up.check_pins(root, not_checked)
    return findings, unmapped, checked, not_checked


def go_feed(*versions_stable):
    """go.dev dl feed body: a list of {version, stable} objects."""
    return json.dumps([{"version": v, "stable": True} for v in versions_stable])


# ── (1) Dockerfile ARG parsing ──────────────────────────────────────────
class ParseDockerfileArgsTest(unittest.TestCase):
    def test_reads_version_args_and_skips_non_versions(self):
        text = (
            "FROM golang:1.24\n"
            "ARG GO_VERSION=1.24.2\n"
            "ARG GH_VERSION=2.63.2\n"
            "ARG RUFF_VERSION=0.8.9\n"
            "ARG REGISTRY=registry.internal.example\n"
            "ARG BASE_TAG=1.24\n"                # ignored: not a tool pin
            "ARG MINIDEB_TAG=1.1\n"              # ignored: not a tool pin
            "ARG GH_COMMIT=" + "a" * 40 + "\n"   # ignored: 40-hex commit id
            "ARG GH_TOKEN_SHA=" + "f" * 40 + "\n"  # ignored: 40-hex commit id
            "ARG GH_IMAGE=registry.internal.example/v${GH_VERSION}\n"  # '$'
            "ARG OLDEST_TOOL=latest\n"           # ignored: literal 'latest'
        )
        self.assertEqual(up.parse_dockerfile_args(text), [
            ("GO_VERSION", "1.24.2"),
            ("GH_VERSION", "2.63.2"),
            ("RUFF_VERSION", "0.8.9"),
        ])

    def test_arg_without_value_is_not_a_pin(self):
        text = (
            "ARG GO_VERSION\n"               # re-declaration without a value
            "ARG GO_VERSION=1.24.2\n"        # the actual pin
            "ARG KUBECTL_VERSION\n"          # no value at all
        )
        self.assertEqual(up.parse_dockerfile_args(text),
                         [("GO_VERSION", "1.24.2")])


# ── (2) Version comparison + bump classification ────────────────────────
class VersionRulesTest(unittest.TestCase):
    def test_compare_and_bump_classification(self):
        self.assertEqual(up.compare_versions("1.24.2", "1.25.0"), -1)
        self.assertEqual(up.compare_versions("1.25.0", "1.24.2"), 1)
        self.assertEqual(up.compare_versions("1.24.2", "1.24.2"), 0)
        self.assertEqual(up.compare_versions("v1.24.2", "1.24.2"), 0)

        self.assertEqual(up.classify_bump("1.24.2", "1.24.3"), "patch")
        self.assertEqual(up.classify_bump("1.24.0", "1.25.0"), "minor")
        self.assertEqual(up.classify_bump("v1.24.0", "v2.0.0"), "major")
        # pin already >= latest, or uncomparable -> no bump
        self.assertIsNone(up.classify_bump("1.25.0", "1.24.2"))
        self.assertIsNone(up.classify_bump("nightly", "1.0.0"))

    def test_pre_release_and_garbage_versions_reject(self):
        for v in ("1.28.0rc1", "1.0beta1", "1.2.0-rc.1",
                  "0.14.3-canary.874", "canary", "nightly", "latest",
                  "not-a-version", "", None):
            self.assertIsNone(up.extract_version(v), msg=repr(v))
            self.assertIsNone(up.compare_versions(v, "1.0.0"), msg=repr(v))
        self.assertEqual(up.extract_version("v0.73.0"), (0, 73, 0))
        self.assertEqual(up.extract_version("1.24.2-ubuntu22.04"), (1, 24, 2))


# ── (3) Stale vs up-to-date Dockerfile pins ─────────────────────────────
class StalePinTest(unittest.TestCase):
    def _feed(self):
        return FakeFeed({
            up.GO_DL_FEED: go_feed("1.25.0", "1.26.0rc1"),  # latest = 1.25.0
            up.OSV_QUERY: json.dumps({"vulns": []}),
        })

    def _run_pin(self, arg, value):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        dockerfile = f"FROM golang:1.24\nARG {arg}={value}\n"
        return run_check_pins(tmp.name, {"Dockerfile.t": dockerfile}, self._feed())

    def test_stale_pin_reported_with_dockerfile_and_arg(self):
        findings, unmapped, checked, not_checked = self._run_pin(
            "GO_VERSION", "1.24.2")
        self.assertEqual(checked, 1)
        self.assertEqual(not_checked, [])
        self.assertEqual(unmapped, [])
        self.assertEqual(len(findings), 1)
        f = findings[0]
        self.assertEqual(f["kind"], "stale_pin")
        self.assertEqual(f["arg"], "GO_VERSION")
        self.assertEqual(f["dockerfile"], "docker/dockerfiles/Dockerfile.t")
        self.assertEqual(f["pinned"], "1.24.2")
        self.assertEqual(f["latest"], "1.25.0")
        self.assertEqual(f["bump"], "minor")
        # Go toolchain moves are human-only and carry the toolchain note
        self.assertTrue(f["human_only"])
        self.assertTrue(any("check-go-toolchain-pins.sh" in n
                            for n in f["coupled"]))

    def test_up_to_date_pin_not_reported(self):
        findings, unmapped, checked, not_checked = self._run_pin(
            "GO_VERSION", "1.25.0")
        self.assertEqual(checked, 1)
        self.assertEqual(findings, [])
        self.assertEqual(unmapped, [])
        self.assertEqual(not_checked, [])


# ── (4) Unmapped pins ───────────────────────────────────────────────────
class UnmappedPinsTest(unittest.TestCase):
    def test_unknown_arg_under_unmapped(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        findings, unmapped, checked, not_checked = run_check_pins(
            tmp.name, {"Dockerfile.x": "ARG FOO_TOOL_VERSION=1.2.3\n"},
            FakeFeed({up.OSV_QUERY: json.dumps({"vulns": []})}))
        self.assertEqual(findings, [])
        self.assertEqual(checked, 0)
        self.assertEqual(len(unmapped), 1)
        u = unmapped[0]
        self.assertEqual(u["arg"], "FOO_TOOL_VERSION")
        self.assertEqual(u["value"], "1.2.3")
        self.assertEqual(u["dockerfiles"], ["docker/dockerfiles/Dockerfile.x"])
        self.assertTrue(u["reason"])


# ── (5) Trivy scanner pin vs latest release ─────────────────────────────
class TrivyPinTest(unittest.TestCase):
    TRIVY_PIN = "v0.48.3\n"

    def _trivy_feed(self, latest_tag, installer_main, installer_pin):
        return FakeFeed({
            TRIVY_LATEST_URL: json.dumps({"tag_name": latest_tag}),
            up.TRIVY_MAIN_INSTALLER: installer_main,
            f"https://raw.githubusercontent.com/aquasecurity/trivy/"
            f"v0.48.3/contrib/install.sh": installer_pin,
        })

    def test_stale_trivy_pin_compared_to_latest_release(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        make_trivy_version(tmp.name, self.TRIVY_PIN)
        not_checked = []
        with patch_feed(self._trivy_feed("v0.50.0", "echo main", "echo main")):
            findings, standing = up.check_trivy(tmp.name, not_checked)
        self.assertEqual(not_checked, [])
        self.assertEqual(standing, [])  # no installer ref in this workflow
        self.assertEqual(len(findings), 1)
        f = findings[0]
        self.assertEqual(f["kind"], "stale_pin")
        self.assertEqual(f["arg"], "TRIVY_VERSION")
        self.assertEqual(f["pinned"], "v0.48.3")
        self.assertEqual(f["latest"], "v0.50.0")
        self.assertEqual(f["bump"], "minor")
        self.assertEqual(f["dockerfile"], "scripts/trivy-version.txt")
        self.assertTrue(f["human_only"])

    def test_up_to_date_trivy_pin_not_reported(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        make_trivy_version(tmp.name, "v0.50.0\n")
        feed = FakeFeed({
            TRIVY_LATEST_URL: json.dumps({"tag_name": "v0.50.0"}),
            up.TRIVY_MAIN_INSTALLER: "echo installer",
            f"https://raw.githubusercontent.com/aquasecurity/trivy/"
            f"v0.50.0/contrib/install.sh": "echo installer",
        })
        not_checked = []
        with patch_feed(feed):
            findings, standing = up.check_trivy(tmp.name, not_checked)
        self.assertEqual(findings, [])
        self.assertEqual(not_checked, [])
        self.assertEqual(standing, [])


# ── (6) Trivy installer refs + sha256 drift main vs pinned tag ──────────
class InstallerCheckTest(unittest.TestCase):
    def _tmp_tree(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        make_worktree(tmp.name, workflows={"ci.yml": "name: ci\n"})
        make_trivy_version(tmp.name, "v0.48.3\n")
        return tmp.name

    def test_ref_listed_with_file_and_line_count(self):
        root = self._tmp_tree()
        wf_dir = os.path.join(root, ".github", "workflows")
        # two ref lines in one workflow + a non-workflow file (ignored)
        with open(os.path.join(wf_dir, "ci.yml"), "w", encoding="utf-8") as f:
            f.write("  - run: curl -fsSL " + up.TRIVY_MAIN_INSTALLER + " | sh\n"
                    "  - run: curl -fsSL " + up.TRIVY_MAIN_INSTALLER + " | sh\n")
        with open(os.path.join(wf_dir, "notes.txt"), "w", encoding="utf-8") as f:
            f.write(up.TRIVY_MAIN_INSTALLER + "\n")  # not a workflow

        refs = up.find_installer_refs(wf_dir)
        self.assertEqual(len(refs), 1)
        self.assertEqual(refs[0][0], ".github/workflows/ci.yml")  # relative to the repo root
        self.assertEqual(refs[0][1], [1, 2])

        feed = self._trivy_feed()
        not_checked = []
        with patch_feed(feed):
            findings, standing = up.check_trivy(root, not_checked)
        self.assertEqual(len(standing), 1)
        note = standing[0]
        self.assertIn("1 workflow file(s), 2 line(s) total", note)
        self.assertIn(".github/workflows/ci.yml", note)
        self.assertIn("L1-2", note)
        self.assertIn("not edited by this script", note)

    def _trivy_feed(self):
        return FakeFeed({
            TRIVY_LATEST_URL: json.dumps({"tag_name": "v0.50.0"}),
            up.TRIVY_MAIN_INSTALLER: "echo installer-main",
            "https://raw.githubusercontent.com/aquasecurity/trivy/"
            "v0.48.3/contrib/install.sh": "echo installer-pinned",
        })

    def test_sha256_drift_between_main_and_pinned_tag_is_a_finding(self):
        root = self._tmp_tree()
        feed = self._trivy_feed()  # installer texts differ
        not_checked = []
        with patch_feed(feed):
            findings, _ = up.check_trivy(root, not_checked)
        self.assertEqual(not_checked, [])
        drift = [f for f in findings
                 if f.get("osv_id") == "trivy-installer-drift"]
        self.assertEqual(len(drift), 1)
        self.assertEqual(drift[0]["kind"], "advisory")
        self.assertIn("sha256", drift[0]["summary"])
        self.assertIn("v0.48.3", drift[0]["summary"])

    def test_equal_installer_text_produces_no_drift_finding(self):
        root = self._tmp_tree()
        feed = FakeFeed({
            TRIVY_LATEST_URL: json.dumps({"tag_name": "v0.50.0"}),
            up.TRIVY_MAIN_INSTALLER: "echo same",
            "https://raw.githubusercontent.com/aquasecurity/trivy/"
            "v0.48.3/contrib/install.sh": "echo same",
        })
        not_checked = []
        with patch_feed(feed):
            findings, _ = up.check_trivy(root, not_checked)
        self.assertEqual(not_checked, [])
        self.assertEqual([f for f in findings
                          if f.get("osv_id") == "trivy-installer-drift"], [])


# ── (7) OSV advisories for a pinned version ─────────────────────────────
class OsvAdvisoryTest(unittest.TestCase):
    ADVISORY = {
        "vulns": [{
            "id": "GO-2024-2882",
            "summary": "net/http: HTTP/2 rapid reset",
            "affects": [{
                "package": {"name": "stdlib", "ecosystem": "Go"},
                "ranges": [{"type": "SEMVER",
                            "events": [{"introduced": "0"},
                                       {"fixed": "1.23.6"}]}],
            }],
        }],
    }

    def test_advisory_for_pinned_version_becomes_finding(self):
        feed = FakeFeed({up.OSV_QUERY: json.dumps(self.ADVISORY)})
        not_checked = []
        with patch_feed(feed):
            advs = up.osv_advisories("Go", "stdlib", "1.23.0", not_checked)
        self.assertEqual(not_checked, [])
        self.assertEqual(len(advs), 1)
        a = advs[0]
        self.assertEqual(a["kind"], "advisory")
        self.assertEqual(a["osv_id"], "GO-2024-2882")
        self.assertEqual(a["package"], "stdlib")
        self.assertEqual(a["pinned"], "1.23.0")
        self.assertEqual(a["fixed"], "1.23.6")
        self.assertIn("rapid reset", a["summary"])
        # the query is a POST to the OSV endpoint with the package named
        call = feed.calls[0]
        self.assertEqual(call["method"], "POST")
        self.assertEqual(call["url"], up.OSV_QUERY)
        self.assertEqual(json.loads(call["body"]),
                         {"package": {"name": "stdlib", "ecosystem": "Go"},
                          "version": "1.23.0"})

    def test_failed_advisory_query_lands_in_not_checked(self):
        feed = FakeFeed({up.OSV_QUERY:
                         up.FeedError(up.OSV_QUERY, "fake: http 500")})
        not_checked = []
        with patch_feed(feed):
            advs = up.osv_advisories("Go", "stdlib", "1.23.0", not_checked)
        self.assertEqual(advs, [])
        self.assertEqual(len(not_checked), 1)
        self.assertEqual(not_checked[0]["url"], up.OSV_QUERY)
        self.assertIn("stdlib", not_checked[0]["detail"])
        self.assertIn("http 500", not_checked[0]["detail"])


# ── (8) Feed failures land in Not checked; other checks still run ───────
class FeedFailureTest(unittest.TestCase):
    def test_failing_feed_not_checked_and_others_still_run(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        dockerfiles = {
            "Dockerfile.a": "ARG GO_VERSION=1.24.2\n",    # feed will fail
            "Dockerfile.b": "ARG RUFF_VERSION=0.8.9\n",   # feed works -> stale
        }
        feed = FakeFeed({
            up.GO_DL_FEED: up.FeedError(up.GO_DL_FEED, "fake: connection reset"),
            "https://pypi.org/pypi/ruff/json":
                json.dumps({"releases": {"0.8.9": {}, "0.9.0": {}}}),
            up.OSV_QUERY: json.dumps({"vulns": []}),
        })
        findings, unmapped, checked, not_checked = run_check_pins(
            tmp.name, dockerfiles, feed)
        self.assertEqual(unmapped, [])
        self.assertEqual(checked, 1)  # the RUFF pin was still checked
        go_failures = [n for n in not_checked if n["url"] == up.GO_DL_FEED]
        self.assertEqual(len(go_failures), 1)
        self.assertIn("connection reset", go_failures[0]["detail"])
        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0]["arg"], "RUFF_VERSION")
        self.assertEqual(findings[0]["bump"], "minor")
        self.assertEqual(findings[0]["dockerfile"],
                         "docker/dockerfiles/Dockerfile.b")


# ── (9) Report shape: Markdown marker + JSON findings_count ─────────────
class ReportShapeTest(unittest.TestCase):
    def _run_watch(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        make_worktree(
            tmp.name,
            dockerfiles={"Dockerfile.a":
                         "FROM golang:1.24\nARG GO_VERSION=1.24.2\n"},
            workflows={"ci.yml": "name: ci\n"})
        make_trivy_version(tmp.name, "v0.48.3\n")
        feed = FakeFeed({
            up.GO_DL_FEED: go_feed("1.25.0"),
            up.OSV_QUERY: json.dumps({"vulns": []}),
            TRIVY_LATEST_URL: json.dumps({"tag_name": "v0.50.0"}),
            up.TRIVY_MAIN_INSTALLER: "echo installer",
            "https://raw.githubusercontent.com/aquasecurity/trivy/"
            "v0.48.3/contrib/install.sh": "echo installer",
        })
        with patch_feed(feed):
            md, jdoc = up.run_watch(tmp.name)
        return md, jdoc

    def test_markdown_report_starts_with_marker(self):
        md, _ = self._run_watch()
        self.assertEqual(md.splitlines()[0], "<!-- upstream-watch -->")
        self.assertIn("## Findings", md)
        self.assertIn("## Not checked (feed failures / unnameable packages)",
                      md)
        self.assertIn("## Last checked", md)

    def test_json_report_findings_count_matches(self):
        _, jdoc = self._run_watch()
        self.assertEqual(jdoc["marker"], "upstream-watch")
        self.assertEqual(jdoc["findings_count"], len(jdoc["findings"]))
        self.assertEqual(jdoc["findings_count"], 2)  # GO stale + Trivy stale
        self.assertEqual(jdoc["checked_pins"], 1)    # only GO_VERSION mapped
        self.assertEqual(jdoc["not_checked"], [])
        kinds = sorted(f["kind"] for f in jdoc["findings"])
        self.assertEqual(kinds, ["stale_pin", "stale_pin"])


# ── (10) End-to-end CLI with --offline-fixtures ─────────────────────────
class CliOfflineEndToEndTest(unittest.TestCase):
    """Runs up.main() on a temporary tree in offline-fixtures mode."""

    def setUp(self):
        # main() sets the module-global FIXTURES; keep it reset afterwards.
        self._saved_fixtures = up.FIXTURES
        self.addCleanup(setattr, up, "FIXTURES", self._saved_fixtures)
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = self.tmp.name
        make_worktree(
            self.root,
            dockerfiles={"Dockerfile.a":
                         "FROM golang:1.24\n"
                         "ARG GO_VERSION=1.24.2\n"
                         "ARG GOPLS_VERSION=0.20.1\n"},
            workflows={"ci.yml": (
                "steps:\n"
                "  - run: |\n"
                "      curl -fsSL "
                "https://raw.githubusercontent.com/aquasecurity/trivy"
                "/main/contrib/install.sh | sh\n")})
        make_trivy_version(self.root, "v0.48.3\n")
        fixtures = {
            up.GO_DL_FEED: go_feed("1.25.0", "1.26.0rc1"),
            up.OSV_QUERY: json.dumps({"vulns": []}),
            TRIVY_LATEST_URL: json.dumps({"tag_name": "v0.50.0"}),
            up.TRIVY_MAIN_INSTALLER: "installer from main",
            # differs from main -> drift finding
            "https://raw.githubusercontent.com/aquasecurity/trivy/"
            "v0.48.3/contrib/install.sh": "installer from pinned tag",
        }
        self.fixtures_path = os.path.join(self.root, "fixtures.json")
        with open(self.fixtures_path, "w", encoding="utf-8") as f:
            json.dump(fixtures, f)
        self.md_path = os.path.join(self.root, "report.md")
        self.json_path = os.path.join(self.root, "report.json")

    def test_offline_run_exits_zero_and_writes_both_reports(self):
        argv = ["--repo-root", self.root,
                "--report-md", self.md_path,
                "--report-json", self.json_path,
                "--offline-fixtures", self.fixtures_path]
        out = io.StringIO()
        with contextlib.redirect_stdout(out):  # keep the summary off test stdout
            rc = up.main(argv)
        self.assertEqual(rc, 0)
        self.assertIn("upstream-watch: 3 finding(s)", out.getvalue())
        # both reports written, and the fixture map was the only feed
        self.assertTrue(os.path.isfile(self.md_path))
        self.assertTrue(os.path.isfile(self.json_path))
        with open(self.md_path, encoding="utf-8") as f:
            md = f.read()
        with open(self.json_path, encoding="utf-8") as f:
            jdoc = json.load(f)
        self.assertEqual(md.splitlines()[0], "<!-- upstream-watch -->")
        self.assertEqual(jdoc["findings_count"], len(jdoc["findings"]))
        # GO stale + Trivy stale + installer drift = exactly 3 findings
        self.assertEqual(jdoc["findings_count"], 3)
        self.assertTrue(any(f.get("bump") == "minor" and
                            f.get("arg") == "GO_VERSION" for f in jdoc["findings"]))
        self.assertTrue(any(f.get("arg") == "TRIVY_VERSION"
                            for f in jdoc["findings"]))
        self.assertTrue(any(f.get("osv_id") == "trivy-installer-drift"
                            for f in jdoc["findings"]))
        # GOPLS_VERSION has no release feed -> reported under unmapped
        self.assertTrue(any(u["arg"] == "GOPLS_VERSION" for u in jdoc["unmapped"]))
        self.assertEqual(jdoc["not_checked"], [])

    def test_missing_repo_root_rejected(self):
        rc = up.main(["--repo-root", os.path.join(self.root, "does-not-exist")])
        self.assertEqual(rc, 2)


if __name__ == "__main__":
    unittest.main()
