#!/usr/bin/env python3
"""Render the weekly dependency-update change description as markdown.

Reads a JSON list on stdin of objects:
  {name, ecosystem, old, new, kind, result, note (optional)}
where kind is security | major | minor | patch and result is
passed | failed | dropped | skipped. Writes markdown on stdout.
An empty list prints nothing and exits 0. Standard library only.
"""
import json
import sys

KIND_ORDER = {"security": 0, "major": 1, "minor": 2, "patch": 3}
RESULTS = ("passed", "failed", "dropped", "skipped")
MAX_NOTE_LINES = 40
HOWTO = "docs/howto/automation-dependency-update.md"


def cell(text):
    """Make text safe for a single markdown table cell."""
    return str(text if text is not None else "").replace("|", "\\|").replace("\r", "").replace("\n", " ")


def trim_lines(text, limit=MAX_NOTE_LINES):
    lines = str(text or "").replace("\r", "").rstrip("\n").split("\n")
    return "\n".join(lines[-limit:])


def sort_key(item):
    return (KIND_ORDER.get(item.get("kind"), len(KIND_ORDER)), str(item.get("name", "")))


def render(items):
    if not items:
        return ""
    items = sorted(items, key=sort_key)
    counts = {r: sum(1 for i in items if i.get("result") == r) for r in RESULTS}
    out = [
        "This run updated %d dependenc%s, dropped %d and skipped %d. "
        "%d failed its tests."
        % (counts["passed"], "y" if counts["passed"] == 1 else "ies",
           counts["dropped"], counts["skipped"], counts["failed"]),
        "",
        "| Dependency | Old version | New version | Kind | Test result |",
        "| --- | --- | --- | --- | --- |",
    ]
    for i in items:
        out.append("| %s | %s | %s | %s | %s |" % (
            cell(i.get("name")), cell(i.get("old")), cell(i.get("new")),
            cell(i.get("kind")), cell(i.get("result"))))
    out.append("")

    attention = [i for i in items if i.get("result") in ("failed", "dropped") or i.get("kind") == "major"]
    if attention:
        out.append("## Needs your attention")
        out.append("")
        for i in attention:
            bad = i.get("result") in ("failed", "dropped")
            label = "%s %s -> %s" % (i.get("name"), i.get("old"), i.get("new"))
            tags = []
            if bad:
                tags.append(i.get("result"))
            if i.get("kind") == "major":
                tags.append("major")
            note = i.get("note")
            if bad and note:
                out.append("- **%s** (%s):" % (cell(label), ", ".join(tags)))
                out.append("")
                out.append("  ```")
                out.extend("  " + l for l in trim_lines(note).split("\n"))
                out.append("  ```")
            elif note:
                out.append("- **%s** (%s): %s" % (cell(label), ", ".join(tags), cell(note)))
            else:
                out.append("- **%s** (%s)" % (cell(label), ", ".join(tags)))
        out.append("")

    out.append("**How to review:** see `%s`." % HOWTO)
    return "\n".join(out) + "\n"


def main():
    try:
        items = json.load(sys.stdin)
    except json.JSONDecodeError as e:
        sys.stderr.write("invalid JSON input: %s\n" % e)
        return 1
    if not isinstance(items, list) or not all(isinstance(i, dict) for i in items):
        sys.stderr.write("input must be a JSON list of objects\n")
        return 1
    sys.stdout.write(render(items))
    return 0


if __name__ == "__main__":
    sys.exit(main())
