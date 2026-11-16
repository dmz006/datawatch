#!/usr/bin/env python3
"""Print the daemon's unlanded-work list, for the supervisor's registry audit.

A thin reader: it calls `datawatch autonomous unlanded-work --json` and
prints the rows and the counts. The daemon does the classification — it
compares branch commits to main by content (git cherry), so a merge that
rewrote commits (a rebase or renumbering) does not leave a branch "ahead"
forever.

  UNLANDED archived, work not on main and nothing shows it was replaced —
           a finding: merge the work or record it as dropped, then delete
           the branch
  PENDING  work not on main, but later work on main replaced it
  live     no settled recommendation yet
  LANDED   every commit is already on main (leftover, safe to delete)

Exit status is 1 when any UNLANDED branch exists, with a summary line at
the end; 0 otherwise; 2 when the daemon read fails.

DATAWATCH_BIN overrides the CLI binary (the default is `datawatch`
resolved from the PATH); tests/test_branch_audit.py feeds a canned daemon
JSON body through it.

usage: scripts/branch-audit.py
"""
import json
import os
import shutil
import subprocess
import sys
from datetime import datetime


def resolve_cli():
    """The datawatch CLI binary: the DATAWATCH_BIN override, else the PATH."""
    return os.environ.get("DATAWATCH_BIN") or shutil.which("datawatch")


def state_of(recommendation):
    """Map a branch recommendation to its landing state (the daemon's own map)."""
    return {
        "landed": "landed",
        "superseded": "pending",
        "needs_decision": "unlanded",
    }.get(recommendation or "", "live")


def load_list(cli):
    """Read the unlanded-work branches from the daemon through the CLI."""
    proc = subprocess.run(
        [cli, "autonomous", "unlanded-work", "--json"],
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        detail = (proc.stderr or proc.stdout or "").strip() or "non-zero exit"
        raise RuntimeError(detail)
    text = proc.stdout[proc.stdout.index("{"):]
    branches = json.loads(text).get("branches")
    return branches or []


def last_text(ts):
    """The last activity as MM-DD HH:MM UTC, or the raw value when unparseable."""
    if not ts:
        return "-"
    try:
        return datetime.fromisoformat(ts.replace("Z", "+00:00")).strftime("%m-%d %H:%M") + " UTC"
    except ValueError:
        return ts


def main():
    cli = resolve_cli()
    if not cli:
        print("branch-audit: no datawatch CLI found on the PATH (set DATAWATCH_BIN to override)", file=sys.stderr)
        return 2
    try:
        branches = load_list(cli)
    except (RuntimeError, ValueError) as e:
        print(f"branch-audit: reading the daemon unlanded-work list failed: {e}", file=sys.stderr)
        return 2

    order = {"unlanded": 0, "pending": 1, "live": 2, "landed": 3}
    rows = sorted(branches, key=lambda b: order[state_of(b.get("recommendation"))])
    for b in rows:
        rec = b.get("recommendation") or "-"
        title = b.get("title") or b.get("automaton_id") or "?"
        reason = f" ({b['reason']})" if b.get("reason") else ""
        print(
            f"{state_of(b.get('recommendation')).upper():<8} "
            f"{b.get('automaton_id') or '?':<8} {rec:<16} "
            f"ahead={b.get('commits', 0):<3} last={last_text(b.get('last_activity'))} "
            f"{title} ({b.get('branch') or '?'}){reason}"
        )
    counts = {k: sum(1 for b in branches if state_of(b.get("recommendation")) == k) for k in order}
    unlanded = counts["unlanded"]
    print(
        f"summary: {unlanded} unlanded (finding), {counts['pending']} pending, "
        f"{counts['live']} live, {counts['landed']} landed"
    )
    if unlanded:
        print(f"FINDING: {unlanded} archived branch(es) with work not on main; merge or record as dropped")
    return 1 if unlanded else 0


if __name__ == "__main__":
    sys.exit(main())
