#!/usr/bin/env python3
"""Regenerate docs/plans/<date>-finish-automation-board.md from the daemon's own data.

The board lists everything still unfinished: finished Automata whose work branch
has commits that never reached main, failed or blocked Automata with work left,
and Automata waiting for review, approval or planning. It reads only the daemon
(`datawatch autonomous merge` and the Automata REST list), so it cannot drift
from reality.

Usage: python3 scripts/automation-board.py [--out PATH] [--url https://127.0.0.1:8443]
"""
import argparse
import datetime
import json
import re
import ssl
import subprocess
import urllib.request

FINISHED_LINE = re.compile(r'Automaton "(.*)" \(id (\w+)\): branch \S+ is (\w+), (\d+) commit\(s\) ahead of (\S+)')


def get(base, path, ctx):
    return json.load(urllib.request.urlopen(base + path, context=ctx))


def finished_with_unlanded_work():
    out = subprocess.run(["datawatch", "autonomous", "merge"], capture_output=True, text=True, timeout=120).stdout
    rows = []
    for line in out.splitlines():
        m = FINISHED_LINE.search(line)
        if m and int(m.group(4)) > 0:
            rows.append((m.group(3), int(m.group(4)), m.group(2), m.group(1), "archived" in line))
    rows.sort(key=lambda r: (r[0] != "pending", -r[1]))
    return rows


def live_automata(base, ctx):
    listing = get(base, "/api/autonomous/prds", ctx)
    listing = listing.get("prds", listing) if isinstance(listing, dict) else listing
    failed_or_blocked, waiting = [], []
    for p in listing:
        status = p.get("status")
        if status not in ("failed", "blocked", "needs_review", "draft", "approved", "running", "waiting"):
            continue
        d = get(base, "/api/autonomous/prds/" + p["id"], ctx)
        d = d.get("prd", d)
        open_tasks = [
            k for s in d.get("stories", []) for k in s.get("tasks", [])
            if (k.get("status") or "pending") not in ("completed", "cancelled", "archived")
        ]
        row = (d["id"], status, len(open_tasks), (d.get("title") or "")[:62], str(d.get("blocked_reason") or "")[:60])
        (failed_or_blocked if status in ("failed", "blocked") else waiting).append(row)
    return failed_or_blocked, waiting


def cell(text):
    return text.replace("|", "/")


def render(unlanded, failed_or_blocked, waiting, now):
    o = ["# Finish-automation board\n"]
    o.append(
        f"Generated {now} from the daemon's own data (merge state and the Automata list) by "
        "`python3 scripts/automation-board.py`. It is the one place that lists everything still "
        "unfinished; the supervisor regenerates it at each full tick.\n"
    )
    o.append("## Rules that keep it organized\n")
    o.append(
        "1. An Automaton is finished only when its work is on main (landing `landed`) or the operator "
        "has recorded `kept` or `dropped` with a reason. Archived is not finished.\n"
        "2. Before filing a new defect or Automaton, run the overlap check; if an Automaton already "
        "describes it, land or finish that one instead.\n"
        "3. Every merge goes through the chain scripts and is recorded shipped only when its branch "
        "is on main.\n"
        "4. The board is regenerated at each hourly tick and any change is committed with the registry row.\n"
    )
    o.append(f"## A. Finished work with commits that never reached main ({len(unlanded)})\n")
    o.append(
        "State `pending` = commits ahead of main, not merged. `needs_look` = diverged or odd, a person "
        "must look. The recommended action is the default; keep and drop need the operator's reason.\n"
    )
    o.append("| State | Commits | Id | Title | Archived | Recommended |\n|---|---:|---|---|---|---|")
    for state, n, ident, title, archived in unlanded:
        rec = "merge through the chain" if state == "pending" else "review the diff, then merge or drop"
        o.append(f"| {state} | {n} | `{ident}` | {cell(title[:70])} | {'yes' if archived else 'no'} | {rec} |")
    o.append(f"\n## B. Failed or blocked with work left ({len(failed_or_blocked)})\n")
    o.append("| Id | Status | Tasks left | Title | Why |\n|---|---|---:|---|---|")
    for r in sorted(failed_or_blocked, key=lambda r: -r[2]):
        o.append(f"| `{r[0]}` | {r[1]} | {r[2]} | {cell(r[3])} | {cell(r[4])} |")
    o.append(f"\n## C. Waiting for review, approval or planning ({len(waiting)})\n")
    o.append("| Id | Status | Tasks | Title |\n|---|---|---:|---|")
    for r in sorted(waiting, key=lambda r: -r[2]):
        o.append(f"| `{r[0]}` | {r[1]} | {r[2]} | {cell(r[3])} |")
    return "\n".join(o) + "\n"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default=None)
    ap.add_argument("--url", default="https://127.0.0.1:8443")
    args = ap.parse_args()
    now = datetime.datetime.now()
    out = args.out or "docs/plans/%s-finish-automation-board.md" % now.strftime("%Y-%m-%d")
    ctx = ssl._create_unverified_context()
    unlanded = finished_with_unlanded_work()
    failed_or_blocked, waiting = live_automata(args.url, ctx)
    with open(out, "w") as f:
        f.write(render(unlanded, failed_or_blocked, waiting, now.strftime("%Y-%m-%d %H:%M")))
    print("wrote %s: %d unlanded, %d failed or blocked, %d waiting" % (out, len(unlanded), len(failed_or_blocked), len(waiting)))


if __name__ == "__main__":
    main()
