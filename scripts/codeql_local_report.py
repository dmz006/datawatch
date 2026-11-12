#!/usr/bin/env python3
"""Summarise local CodeQL SARIF results and compare them with GitHub alerts.

Reads every *.sarif file in a directory and reduces each result to rule id,
path, start line, level and message. The level comes from the rule's
`security-severity` property: 9.0 or more is critical, 7.0 or more high,
4.0 or more medium, anything else (or no value) low.

With --alerts-json FILE (a GitHub code-scanning alerts list) each result is
matched by rule, path and line to an alert and marked open on GitHub,
dismissed on GitHub or not on GitHub. The exit status mirrors the CI alert
gate: 1 if any high or critical result is not matched to a dismissed alert,
else 0. Without an alerts file every high or critical result counts.

Standard library only; run as `python3 -I scripts/codeql_local_report.py`.
"""

import argparse
import collections
import glob
import json
import os
import sys

OPEN = "open on GitHub"
DISMISSED = "dismissed on GitHub"
NOT_ON_GITHUB = "not on GitHub"
BLOCKING_LEVELS = ("critical", "high")


def severity_level(value):
    """Map a security-severity value (number or string) to a level name."""
    try:
        score = float(value)
    except (TypeError, ValueError):
        return "low"
    if score >= 9.0:
        return "critical"
    if score >= 7.0:
        return "high"
    if score >= 4.0:
        return "medium"
    return "low"


def _rule_severity(rule):
    props = rule.get("properties") or {}
    return props.get("security-severity")


def parse_sarif(doc):
    """Return a list of result dicts from one parsed SARIF document."""
    results = []
    for run in doc.get("runs") or []:
        tool = run.get("tool") or {}
        components = [tool.get("driver") or {}] + list(tool.get("extensions") or [])
        severity_by_id = {}
        for comp in components:
            for rule in comp.get("rules") or []:
                severity_by_id.setdefault(rule.get("id"), _rule_severity(rule))
        driver_rules = (tool.get("driver") or {}).get("rules") or []
        for res in run.get("results") or []:
            rule_id = res.get("ruleId") or (res.get("rule") or {}).get("id")
            severity = severity_by_id.get(rule_id)
            index = res.get("ruleIndex")
            if severity is None and isinstance(index, int) and 0 <= index < len(driver_rules):
                severity = _rule_severity(driver_rules[index])
            path, line = "", 0
            locations = res.get("locations") or []
            if locations:
                phys = locations[0].get("physicalLocation") or {}
                path = (phys.get("artifactLocation") or {}).get("uri", "")
                line = (phys.get("region") or {}).get("startLine", 0)
            results.append({
                "rule": rule_id,
                "path": path,
                "line": line,
                "level": severity_level(severity),
                "message": (res.get("message") or {}).get("text", ""),
            })
    return results


def load_sarif_dir(directory):
    results = []
    for name in sorted(glob.glob(os.path.join(directory, "*.sarif"))):
        with open(name, encoding="utf-8") as fh:
            results.extend(parse_sarif(json.load(fh)))
    return results


def load_alerts(path):
    """Read a GitHub alerts list; accepts several concatenated JSON arrays
    (the output of `gh api --paginate` run once per state)."""
    with open(path, encoding="utf-8") as fh:
        text = fh.read()
    decoder = json.JSONDecoder()
    alerts, pos = [], 0
    while True:
        while pos < len(text) and text[pos].isspace():
            pos += 1
        if pos >= len(text):
            break
        value, pos = decoder.raw_decode(text, pos)
        alerts.extend(value if isinstance(value, list) else [value])
    return alerts


def alert_index(alerts):
    """Map (rule, path, line) to the best alert state; open beats dismissed."""
    index = {}
    for alert in alerts:
        state = alert.get("state")
        if state not in ("open", "dismissed"):
            continue
        loc = (alert.get("most_recent_instance") or {}).get("location") or {}
        key = ((alert.get("rule") or {}).get("id"), loc.get("path"), loc.get("start_line"))
        if index.get(key) != "open":
            index[key] = state
    return index


def classify(results, alerts):
    """Add a `github` status to every result. alerts None means no comparison."""
    index = alert_index(alerts) if alerts is not None else None
    for res in results:
        if index is None:
            res["github"] = None
            continue
        state = index.get((res["rule"], res["path"], res["line"]))
        res["github"] = {"open": OPEN, "dismissed": DISMISSED}.get(state, NOT_ON_GITHUB)
    return results


def blocking(results):
    """High or critical results that are not matched to a dismissed alert."""
    return [r for r in results
            if r["level"] in BLOCKING_LEVELS and r["github"] != DISMISSED]


def report(results, compared, out=None):
    out = out or sys.stdout
    by_rule = collections.Counter((r["rule"], r["level"]) for r in results)
    print("CodeQL local results: %d" % len(results), file=out)
    for (rule, level), count in sorted(by_rule.items(), key=lambda kv: (-kv[1], kv[0][0] or "")):
        print("  %5d  %-8s %s" % (count, level, rule), file=out)
    flagged = [r for r in results if r["level"] in BLOCKING_LEVELS]
    print("", file=out)
    print("High and critical results: %d" % len(flagged), file=out)
    for res in sorted(flagged, key=lambda r: (r["level"] != "critical", r["rule"] or "", r["path"], r["line"])):
        status = res["github"] if compared else "not compared"
        print("  [%s] %s %s:%s (%s)" % (res["level"], res["rule"], res["path"], res["line"], status), file=out)
    bad = blocking(results)
    if compared:
        print("", file=out)
        print("Not dismissed on GitHub (the alert gate would count these): %d" % len(bad), file=out)
    else:
        print("", file=out)
        print("No GitHub alerts to compare with: every high or critical result counts (%d)." % len(bad), file=out)
    return bad


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("sarif_dir", help="directory holding the *.sarif files")
    parser.add_argument("--alerts-json", metavar="FILE",
                        help="GitHub code-scanning alerts list to compare with")
    args = parser.parse_args(argv)

    results = load_sarif_dir(args.sarif_dir)
    alerts = load_alerts(args.alerts_json) if args.alerts_json else None
    classify(results, alerts)
    bad = report(results, alerts is not None)
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
