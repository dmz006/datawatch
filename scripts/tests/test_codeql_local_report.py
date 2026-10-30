#!/usr/bin/env python3
"""Unit tests for scripts/codeql_local_report.py.

Run: python3 -I scripts/tests/test_codeql_local_report.py
"""

import contextlib
import importlib.util
import io
import json
import os
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
MODULE_PATH = os.path.join(HERE, "..", "codeql_local_report.py")

spec = importlib.util.spec_from_file_location("codeql_local_report", MODULE_PATH)
report = importlib.util.module_from_spec(spec)
spec.loader.exec_module(report)


def sarif(severity, rule_id="go/example", path="a/b.go", line=7):
    props = {} if severity is None else {"security-severity": severity}
    return {
        "version": "2.1.0",
        "runs": [{
            "tool": {"driver": {"name": "CodeQL", "rules": [{"id": rule_id, "properties": props}]}},
            "results": [{
                "ruleId": rule_id,
                "ruleIndex": 0,
                "message": {"text": "something is wrong"},
                "locations": [{"physicalLocation": {
                    "artifactLocation": {"uri": path},
                    "region": {"startLine": line},
                }}],
            }],
        }],
    }


def alert(state, rule_id="go/example", path="a/b.go", line=7):
    return {
        "rule": {"id": rule_id},
        "state": state,
        "most_recent_instance": {"location": {"path": path, "start_line": line}},
    }


class ReportTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)

    def run_report(self, severity, alerts=None, **kw):
        with open(os.path.join(self.tmp.name, "go.sarif"), "w", encoding="utf-8") as fh:
            json.dump(sarif(severity, **kw), fh)
        argv = [self.tmp.name]
        if alerts is not None:
            alerts_path = os.path.join(self.tmp.name, "alerts.json")
            with open(alerts_path, "w", encoding="utf-8") as fh:
                json.dump(alerts, fh)
            argv += ["--alerts-json", alerts_path]
        with contextlib.redirect_stdout(io.StringIO()):
            return report.main(argv)

    def test_severity_mapping(self):
        self.assertEqual(report.severity_level("9.0"), "critical")
        self.assertEqual(report.severity_level(9.8), "critical")
        self.assertEqual(report.severity_level("8.9"), "high")
        self.assertEqual(report.severity_level("7.0"), "high")
        self.assertEqual(report.severity_level("6.9"), "medium")
        self.assertEqual(report.severity_level("4.0"), "medium")
        self.assertEqual(report.severity_level("3.9"), "low")
        self.assertEqual(report.severity_level(None), "low")

    def test_missing_severity_counts_as_low(self):
        results = report.parse_sarif(sarif(None))
        self.assertEqual(results[0]["level"], "low")
        self.assertEqual(self.run_report(None), 0)

    def test_parse_fields(self):
        res = report.parse_sarif(sarif("7.5"))[0]
        self.assertEqual(
            (res["rule"], res["path"], res["line"], res["level"], res["message"]),
            ("go/example", "a/b.go", 7, "high", "something is wrong"),
        )

    def test_high_without_alert_fails(self):
        self.assertEqual(self.run_report("8.0", alerts=[]), 1)

    def test_high_without_alerts_file_fails(self):
        self.assertEqual(self.run_report("8.0"), 1)

    def test_high_matched_to_dismissed_alert_passes(self):
        self.assertEqual(self.run_report("8.0", alerts=[alert("dismissed")]), 0)

    def test_high_matched_to_open_alert_fails(self):
        self.assertEqual(self.run_report("8.0", alerts=[alert("open")]), 1)

    def test_open_beats_dismissed_for_same_key(self):
        self.assertEqual(self.run_report("9.5", alerts=[alert("dismissed"), alert("open")]), 1)

    def test_alert_at_other_line_does_not_match(self):
        self.assertEqual(self.run_report("8.0", alerts=[alert("dismissed", line=8)]), 1)

    def test_medium_never_fails(self):
        self.assertEqual(self.run_report("5.0", alerts=[]), 0)
        self.assertEqual(self.run_report("5.0"), 0)

    def test_concatenated_alert_pages(self):
        path = os.path.join(self.tmp.name, "pages.json")
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(json.dumps([alert("open", line=1)]) + "\n" + json.dumps([alert("dismissed")]))
        self.assertEqual(len(report.load_alerts(path)), 2)

    def test_status_labels(self):
        results = report.parse_sarif(sarif("8.0"))
        report.classify(results, [alert("open")])
        self.assertEqual(results[0]["github"], report.OPEN)
        results = report.parse_sarif(sarif("8.0"))
        report.classify(results, [alert("dismissed")])
        self.assertEqual(results[0]["github"], report.DISMISSED)
        results = report.parse_sarif(sarif("8.0"))
        report.classify(results, [])
        self.assertEqual(results[0]["github"], report.NOT_ON_GITHUB)


if __name__ == "__main__":
    unittest.main()
