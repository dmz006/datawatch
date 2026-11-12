#!/usr/bin/env python3
"""List automaton/* branches with commits not in main, by content.

A branch's commit count ahead of main is misleading: merges that rewrite
commits (renumbering, rebases) leave the branch "ahead" forever. This
compares commit subjects instead, and joins the Automaton status, so a
finished Automaton whose work never reached main stands out from a leftover
branch.

  landed   every subject is already on main (leftover, safe to delete)
  PENDING  Automaton completed and some subjects are not on main (merge
           pending or a dropped branch the registry must explain)
  live     Automaton still running or planning
  UNLANDED archived (cancelled, blocked, failed or no record) and some
           subjects are not on main. A finding: merge the work or record
           it as dropped, then delete the branch.

Exit status is 1 when any UNLANDED branch exists, with a summary line at
the end; 0 otherwise.

usage: scripts/branch-audit.py
"""
import json
import subprocess
import sys


def sh(*a):
    return subprocess.run(a, capture_output=True, text=True).stdout


def prd_status():
    out = sh("datawatch", "autonomous", "prd-list")
    try:
        data = json.loads(out[out.index("{"):])
    except ValueError:
        return {}
    return {p["id"]: (p["status"], (p.get("title") or p.get("spec", ""))[:50]) for p in data.get("prds", [])}


def main():
    st = prd_status()
    rows = []
    for b in sh("git", "branch", "--list", "automaton/*", "--format=%(refname:short)").split():
        ahead = int(sh("git", "rev-list", "--count", "main.." + b) or 0)
        if not ahead:
            continue
        pid = b.split("/", 1)[1]
        subs = [s for s in sh("git", "log", "--format=%s", "main.." + b).split("\n") if s]
        landed = sum(1 for s in subs if sh("git", "log", "main", "--fixed-strings", "--grep=" + s, "-1", "--format=%h").strip())
        last = sh("git", "log", "-1", "--format=%cd", "--date=format:%m-%d %H:%M", b).strip()
        status, title = st.get(pid, ("(no Automaton record)", ""))
        if landed == len(subs):
            kind = "landed"
        elif status == "completed":
            kind = "PENDING"
        elif status in ("running", "planning"):
            kind = "live"
        else:
            kind = "UNLANDED"
        rows.append((kind, pid, status, ahead, landed, last, title))
    order = {"UNLANDED": 0, "PENDING": 1, "live": 2, "landed": 3}
    for kind, pid, status, ahead, landed, last, title in sorted(rows, key=lambda r: order[r[0]]):
        print(f"{kind:<8} {pid} {status:<16} ahead={ahead:<3} on_main={landed}/{ahead} last={last} {title}")
    counts = {k: sum(1 for r in rows if r[0] == k) for k in order}
    unlanded = counts["UNLANDED"]
    print(
        f"summary: {unlanded} unlanded (finding), {counts['PENDING']} pending, "
        f"{counts['live']} live, {counts['landed']} landed"
    )
    if unlanded:
        print(f"FINDING: {unlanded} archived branch(es) with work not on main; merge or record as dropped")
    return 1 if unlanded else 0


if __name__ == "__main__":
    sys.exit(main())
