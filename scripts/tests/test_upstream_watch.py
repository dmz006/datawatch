#!/usr/bin/env python3
"""Unit tests for scripts/upstream_watch.py.

Run (from the repository root, or the tests directory):
    python3 -I -m unittest scripts/tests/test_upstream_watch.py

No test touches the network: every test that drives the check functions
patches upstream_watch.fetch with a fake, and the end-to-end CLI test goes
through the --offline-fixtures path. All repository trees are built under
tempfile.TemporaryDirectory; nothing is written into the real repo.
"""

import json
import os
import sys
import tempfile
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPTS_DIR = os.path.dirname(HERE)
sys.path.insert(0, SCRIPTS_DIR)

import upstream_watch as up  # noqa: E402


def fake_fetch(responses):
    """Build a stand-in for upstream_watch.fetch.

    `responses` maps URL -> response body text, or -> an Exception
    instance to raise. A URL missing from the map raises FeedError, the
    same way missing offline fixtures do."""
    calls = []

    def _fetch(url, method="GET", body=None):
        calls.append((method, url, body))
        if url not in responses:
            raise up.FeedError(url, "fake: no response registered")
        value = responses[url]
        if isinstance(value, Exception):
            raise value
        return value

    _fetch.calls = calls
    return _fetch


def build_repo(tmp, dockerfiles=None, workflows=None, dockerfile_dev=None):
    """Create a minimal repository tree under `tmp`."""
    if dockerfiles:
        d = os.path.join(tmp, "docker", "dockerfiles")
        os.makedirs(d)
        for name, text in dockerfiles.items():
            with open(os.path.join(d, name), "w", encoding="utf-8") as f:
                f.write(text)
    if workflows:
        wd = os.path.join(tmp, ".github", "workflows")
        os.makedirs(wd)
        for name, text in workflows.items():
            with open(os.path.join(wd, name), "w", encoding="utf-8") as f:
                f.write(text)
    if dockerfile_dev is not None:
        with open(os.path.join(tmp, "Dockerfile.dev"), "w", encoding="utf-8") as f:
            f.write(dockerfile_dev)
    return tmp


class Replacable:
    """Mixin providing run_check_pins() that also exposes not_checked."""

    def run_check_pins(self, tmp, dockerfiles, responses):
        build_repo(tmp.name, dockerfiles=dockerfiles)
        not_checked = []
        with mock.patch.object(up, "fetch", fake_fetch(responses)):
            result = up.check_pins(tmp.name, not_checked)
        self.not_checked = not_checked
        return result


class ParseDockerfileArgsTest(unittest.TestCase):
    def test_reads_version_args_and_skips_non_versions(self):
        text = (
            "FROM golang:1.24\n"
            "ARG GO_VERSION=1.24.2\n"
            "ARG GH_VERSION=2.63.2\n"
            "ARG RUST_VERSION=1.85.0\n"
            "ARG REGISTRY=registry.internal.example\n"
            "ARG BASE_TAG=1.24\n"              # ignored: not a tool pin
            "ARG MINIDEB_TAG=1.1\n"            # ignored: not a tool pin
            "ARG GH_COMMIT=" + "a" * 40 + "\n"  # ignored: commit id
            "ARG BUILD_BASE=v${GO_VERSION}\n"   # ignored: value contains $
            "ARG COMMIT_A=" + "b" * 40 + "\n"   # ignored: 40-hex commit id
            "ARG OLDEST_TOOL=latest\n"          # ignored: literal latest
        )
        pins = up.parse_dockerfile_args(text)
        self.assertEqual(pins, [
            ("GO_VERSION", "1.24.2"),
            ("GH_VERSION", "2.63.2"),
            ("RUST_VERSION", "1.85.0"),
        ])


class VersionCompareTest(unittest.TestCase):
    def test_compare_and_bump_classification(self):
        self.assertEqual(up.compare_versions("1.24.2", "1.25.0"), -1)
        self.assertEqual(up.compare_versions("1.25.0", "1.24.2"), 1)
        self.assertEqual(up.compare_versions("1.24.2", "1.24.2"), 0)
        self.assertEqual(up.compare_versions("v1.24.2", "1.24.2"), 0)

        self.assertEqual(up.classify_bump("1.24.2", "1.24.3"), "patch")
        self.assertEqual(up.classify_bump("1.24.0", "1.25.0"), "minor")
        self.assertEqual(up.classify_bump("v1.24.0", "v2.0.0"), "major")
        # pin already >= latest (or uncomparable) -> no bump
        self.assertIsNone(up.classify_bump("1.25.0", "1.24.2"))
        self.assertIsNone(up.classify_bump("nightly", "1.0.0"))

    def test_pre_release_and_unparseable_versions_reject(self):
        for v in ("1.28.0rc1", "1.0beta1", "1.2.0-rc.1",
                  "0.14.3-canary.874", "canary", "nightly", "latest", "", None):
            self.assertIsNone(up.extract_version(v), msg=repr(v))
            self.assertIsNone(up.compare_versions(v, "1.0.0"), msg=repr(v))
        self.assertEqual(up.extract_version("v0.73.0"), (0, 73, 0))
        self.assertEqual(up.extract_version("1.24.2-ubuntu22.04"), (1, 24, 2))


class StalePinTest(Replacable, unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.responses = {
            up.GO_DL_FEED: json.dumps(
                [{"version": "1.25.0", "stable": True},
                 {"version": "1.26.0rc1", "stable": True}]),
            up.OSV_QUERY: json.dumps({"vulns": []}),
        }

    def test_stale_pin_reported_with_dockerfile_and_arg(self):
        findings, unmapped, checked = self.run_check_pins(
            self.tmp, {"Dockerfile.stale": "FROM golang:1.24\nARG GO_VERSION=1.24.2\n"},
            self.responses)
        self.assertEqual(checked, 1)
        self.assertEqual(len(findings), 1)
        f = findings[0]
        self.assertEqual(f["kind"], "stale_pin")
        self.assertEqual(f["arg"], "GO_VERSION")
        self.assertEqual(f["dockerfile"], "docker/dockerfiles/Dockerfile.stale")
        self.assertEqual(f["pinned"], "1.24.2")
        self.assertEqual(f["latest"], "1.25.0")
        self.assertEqual(f["bump"], "minor")

    def test_up_to_date_pin_not_reported(self):
        findings, unmapped, checked = self.run_check_pins(
            self.tmp, {"Dockerfile.cur": "FROM golang:1.25\nARG GO_VERSION=1.25.0\n"},
            self.responses)
        self.assertEqual(checked, 1)
        self.assertEqual(findings, [])
        self.assertEqual(unmapped, [])


class UnmappedPinTest(Replacable, unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)

    def test_arg_without_known_source_under_unmapped(self):
        findings, unmapped, checked = self.run_check_pins(
            self.tmp, {"Dockerfile.x": "ARG FOO_TOOL_VERSION=1.2.3\n"}, {})
        self.assertEqual(findings, [])
        self.assertEqual(checked, 0)
        self.assertEqual(len(unmapped), 1)
        u = unmapped[0]
        self.assertEqual(u["arg"], "FOO_TOOL_VERSION")
        self.assertEqual(u["value"], "1.2.3")
        self.assertIn("docker/dockerfiles/Dockerfile.x", u["dockerfiles"])
        self.assertTrue(u["reason"])


class TrivyWorkflowTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.workflows = {"trivy.yml": (
            "env:\n"
            "  TRIVY_VERSION: v0.48.3\n"
            "steps:\n"
            "  - run: |\n"
            "      curl -fsSL "
            "https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh\n")}
        build_repo(self.tmp.name, workflows=self.workflows)

    def test_trivy_pin_compared_to_latest_release(self):
        responses = {
            "https://api.github.com/repos/aquasecurity/trivy/releases/latest":
                json.dumps({"tag_name": "v0.50.0"}),
            up.TRIVY_MAIN_INSTALLER: "echo installer-main",
            "https://raw.githubusercontent.com/aquasecurity/trivy/v0.48.3/contrib/install.sh":
                "echo installer-main",
        }
        not_checked = []
        with mock.patch.object(up, "fetch", fake_fetch(responses)):
            findings, standing = up.check_trivy(self.tmp.name, not_checked)
        self.assertEqual(not_checked, [])
        stale = [f for f in findings if f["kind"] == "stale_pin"]
        self.assertEqual(len(stale), 1)
        self.assertEqual(stale[0]["arg"], "TRIVY_VERSION")
        self.assertEqual(stale[0]["pinned"], "v0.48.3")
        self.assertEqual(stale[0]["latest"], "v0.50.0")
        self.assertIn(".github/workflows/trivy.yml", stale[0]["dockerfile"])
        self.assertEqual(stale[0]["human_only"], True)

    def test_up_to_date_trivy_pin_not_reported(self):
        responses = {
            "https://api.github.com/repos/aquasecurity/trivy/releases/latest":
                json.dumps({"tag_name": "v0.50.0"}),
            up.TRIVY_MAIN_INSTALLER: "echo installer",
            "https://raw.githubusercontent.com/aquasecurity/trivy/v0.50.0/contrib/install.sh":
                "echo installer",
        }
        workflows = {"trivy.yml": "env:\n  TRIVY_VERSION: v0.50.0\n"}
        build_repo(self.tmp.name + "2", workflows=workflows)
        not_checked = []
        with mock.patch.object(up, "fetch", fake_fetch(responses)):
            findings, _ = up.check_trivy(self.tmp.name + "2", not_checked)
        self.assertEqual(findings, [])


class InstallerRefsTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)

    def _workflows(self, installer_text_main, installer_text_pin):
        responses = {
            "https://api.github.com/repos/aquasecurity/trivy/releases/latest":
                json.dumps({"tag_name": "v0.50.0"}),
            up.TRIVY_MAIN_INSTALLER: installer_text_main,
            "https://raw.githubusercontent.com/aquasecurity/trivy/v0.48.3/contrib/install.sh":
                installer_text_pin,
        }
        return responses

    def test_installer_refs_listed_with_file_and_count(self):
        wf_dir = os.path.join(self.tmp.name, ".github", "workflows")
        os.makedirs(wf_dir)
        with open(os.path.join(wf_dir, "ci.yml"), "w", encoding="utf-8") as f:
            f.write("steps:\n"
                    "  - run: curl -fsSL " + up.TRIVY_MAIN_INSTALLER + " | sh\n"
                    "  - run: curl -fsSL " + up.TRIVY_MAIN_INSTALLER + " | sh\n")
        with open(os.path.join(wf_dir, "other.txt"), "w", encoding="utf-8") as f:
            f.write(up.TRIVY_MAIN_INSTALLER + "\n")  # non-workflow file ignored
        refs = up.find_installer_refs(wf_dir)
        self.assertEqual(len(refs), 1)
        self.assertEqual(refs[0][0], ".github/workflows/ci.yml")
        self.assertEqual(sorted(refs[0][1]), [2, 3])
        # and the standing note carries file + count
        with mock.patch.object(up, "fetch",
                               fake_fetch({"https://api.github.com/x": "{}"})):
            findings, standing = up.check_trivy(self.tmp.name, [])
        self.assertIn("main", standing[0])
        self.assertIn("ci.yml (2 lines, L2-3)", standing[0])
        self.assertIn("1 workflow file(s), 2 line(s) total", standing[0])

    def test_sha256_drift_between_main_and_pinned_tag(self):
        tmp2 = tempfile.TemporaryDirectory()
        self.addCleanup(tmp2.cleanup)
        build_repo(tmp2.name, workflows={"trivy.yml": "env:\n  TRIVY_VERSION: v0.48.3\n"})
        with mock.patch.object(up, "fetch",
                               fake_fetch(self._workflows("echo main-new",
                                                          "echo pinned-old"))):
            findings, _ = up.check_trivy(tmp2.name, [])
        drift = [f for f in findings if f.get("osv_id") == "trivy-installer-drift"]
        self.assertEqual(len(drift), 1)
        self.assertIn("sha256", drift[0]["summary"])
        # equal installer text does NOT produce a drift finding
        with mock.patch.object(up, "fetch",
                               fake_fetch(self._workflows("echo same", "echo same"))):
            findings2, _ = up.check_trivy(tmp2.name, [])
        self.assertEqual(
            [f for f in findings2 if f.get("osv_id") == "trivy-installer-drift"], [])


class OsvAdvisoryTest(unittest.TestCase):
    def test_advisory_for_pinned_version_becomes_finding(self):
        osv_payload = {
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
        responses = {up.OSV_QUERY: json.dumps(osv_payload)}
        calls = fake_fetch(responses)
        with mock.patch.object(up, "fetch", calls):
            advs = up.osv_advisories("Go", "stdlib", "1.23.0", not_checked=[])
        self.assertEqual(len(advs), 1)
        self.assertEqual(advs[0]["kind"], "advisory")
        self.assertEqual(advs[0]["osv_id"], "GO-2024-2882")
        self.assertEqual(advs[0]["pinned"], "1.23.0")
        self.assertEqual(advs[0]["fixed"], "1.23.6")
        self.assertEqual(calls.calls[0][0], "POST")
        self.assertEqual(calls.calls[0][1], up.OSV_QUERY)

    def test_advisory_query_failure_lands_in_not_checked(self):
        responses = {up.OSV_QUERY: up.FeedError(up.OSV_QUERY, "fake: 500")}
        not_checked = []
        with mock.patch.object(up, "fetch", fake_fetch(responses)):
            advs = up.osv_advisories("Go", "stdlib", "1.23.0", not_checked)
        self.assertEqual(advs, [])
        self.assertEqual(len(not_checked), 1)
        self.assertEqual(not_checked[0]["url"], up.OSV_QUERY)


class FeedFailureTest(Replacable, unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)

    def test_failing_feed_not_checked_and_others_run(self):
        responses = {
            up.GO_DL_FEED: up.FeedError(up.GO_DL_FEED, "fake: connection reset"),
            "https://pypi.org/pypi/ruff/json": json.dumps(
                {"releases": {"0.8.9": {}, "0.9.0": {}}}),
            up.OSV_QUERY: json.dumps({"vulns": []}),
        }
        dockerfiles = {
            "Dockerfile.a": "ARG GO_VERSION=1.24.2\n",   # feed fails
            "Dockerfile.b": "ARG RUFF_VERSION=0.8.9\n",  # feed works -> stale pin
        }
        findings, unmapped, checked = self.run_check_pins(
            self.tmp, dockerfiles, responses)
        self.assertEqual(checked, 1)  # the RUFF pin was still checked
        self.assertTrue(any(n["url"] == up.GO_DL_FEED for n in self.not_checked))
        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0]["arg"], "RUFF_VERSION")


class ReportBuildTest(unittest.TestCase):
    def test_markdown_marker_and_json_findings_count(self):
        with tempfile.TemporaryDirectory() as tmp:
            build_repo(tmp, dockerfiles={"Dockerfile.a": "ARG GO_VERSION=1.24.2\n"},
                       workflows={"trivy.yml": "env:\n  TRIVY_VERSION: v0.48.3\n"})
            responses = {
                up.GO_DL_FEED: json.dumps([{"version": "1.25.0", "stable": True}]),
                up.OSV_QUERY: json.dumps({"vulns": []}),
                "https://api.github.com/repos/aquasecurity/trivy/releases/latest":
                    json.dumps({"tag_name": "v0.50.0"}),
                up.TRIVY_MAIN_INSTALLER: "echo same",
                "https://raw.githubusercontent.com/aquasecurity/trivy/v0.48.3/contrib/install.sh":
                    "echo same",
            }
            with mock.patch.object(up, "fetch", fake_fetch(responses)):
                md, jdoc = up.run_watch(tmp)
        self.assertTrue(md.startswith("<!-- upstream-watch -->"), md[:80])
        self.assertGreater(jdoc["findings_count"], 0)
        self.assertEqual(jdoc["findings_count"], len(jdoc["findings"]))
        self.assertEqual(jdoc["marker"], "upstream-watch")
        self.assertIn("upstream-watch", md)


class CliOfflineTest(unittest.TestCase):
    """End-to-end run of main() on a temporary tree with --offline-fixtures."""

    def test_cli_offline_end_to_end(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        root = tmp.name
        build_repo(
            root,
            dockerfiles={"Dockerfile.a":
                         "FROM golang:1.24\nARG GO_VERSION=1.24.2\nARG GOPLS_VERSION=0.20.1\n"},
            workflows={"ci.yml": (
                "env:\n"
                "  TRIVY_VERSION: v0.48.3\n"
                "steps:\n"
                "  - run: |\n"
                "      curl -fsSL " + up.TRIVY_MAIN_INSTALLER + " | sh\n")})
        fixtures = {
            up.GO_DL_FEED: json.dumps(
                [{"version": "1.25.0", "stable": True},
                 {"version": "1.26.0rc1", "stable": True}]),
            up.OSV_QUERY: json.dumps({"vulns": []}),
            "https://api.github.com/repos/aquasecurity/trivy/releases/latest":
                json.dumps({"tag_name": "v0.50.0"}),
            up.TRIVY_MAIN_INSTALLER: "echo installer",
            "https://raw.githubusercontent.com/aquasecurity/trivy/v0.48.3/contrib/install.sh":
                "echo installer-diff",  # differs from main -> drift finding
        }
        fixtures_path = os.path.join(root, "fixtures.json")
        with open(fixtures_path, "w", encoding="utf-8") as f:
            json.dump(fixtures, f)
        md_path = os.path.join(root, "report.md")
        json_path = os.path.join(root, "report.json")
        try:
            rc = up.main(["--repo-root", root,
                          "--report-md", md_path,
                          "--report-json", json_path,
                          "--offline-fixtures", fixtures_path])
            self.assertEqual(rc, 0)
        finally:
            # restore the module to its offline-invisible state for later tests
            up.FIXTURES = None
        with open(md_path, encoding="utf-8") as f:
            md = f.read()
        with open(json_path, encoding="utf-8") as f:
            jdoc = json.load(f)
        self.assertTrue(md.startswith("<!-- upstream-watch -->"))
        # GO stale pin + trivy stale pin + trivy installer drift finding
        self.assertGreaterEqual(jdoc["findings_count"], 3)
        self.assertEqual(jdoc["findings_count"], len(jdoc["findings"]))
        self.assertTrue(any(f.get("osv_id") == "trivy-installer-drift"
                            for f in jdoc["findings"]))
        self.assertTrue(any(unm["arg"] == "GOPLS_VERSION" for unm in jdoc["unmapped"]))


if __name__ == "__main__":
    unittest.main()
