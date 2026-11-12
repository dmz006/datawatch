#!/usr/bin/env python3
"""scripts/overall-counts.py - overall Automata counts and details for the tick.

Reads `datawatch autonomous prd-list` once and prints the whole history, not
only the open work that scripts/v10-status.py covers:

  * records by status, and what counts as done: a completed record, or an
    archived or cancelled record whose tasks all completed (the supervisor
    loop archives merged work, so finished Automata are mostly archived)
  * tasks done and open, attempts, retries, failed attempts and worker time
  * by kind, by backend, by Automation (arc) and by project
  * the last days: Automata and tasks finished per day
  * releases since the first tag of the line

Project grouping needs a real project on the record; today the record only
carries the Automaton's worktree path, so every record is reported under
"(none)" and counted, never dropped. Once records carry a project the same
grouping fills in.

Usage: python3 scripts/overall-counts.py [--json] [--days N] [--prds-file PATH]
Stdlib only.
"""
import argparse
import json
import subprocess
import sys
from collections import Counter, defaultdict
from datetime import datetime, timedelta, timezone


def load(path):
    if path:
        raw = open(path).read()
    else:
        raw = subprocess.run(["datawatch", "autonomous", "prd-list"],
                             capture_output=True, text=True).stdout
    return json.JSONDecoder().raw_decode(raw)[0].get("prds", [])


def ts(v):
    if not v:
        return None
    try:
        return datetime.fromisoformat(v.replace("Z", "+00:00"))
    except ValueError:
        return None


def tasks_of(p):
    return [t for s in (p.get("stories") or []) for t in (s.get("tasks") or [])]


def classify(p):
    ts_ = tasks_of(p)
    st = p.get("status")
    if st == "completed":
        return "done"
    if st in ("archived", "cancelled") and ts_ and all(t.get("status") == "completed" for t in ts_):
        return "done"
    if st in ("archived", "cancelled", "rejected"):
        return "abandoned"
    return "open"


def hours(sec):
    return round(sec / 3600.0, 1)


def build(prds, days):
    cls = {p["id"]: classify(p) for p in prds}
    out = {"records": len(prds), "by_status": dict(Counter(p.get("status") for p in prds)),
           "by_class": dict(Counter(cls.values()))}
    first = min((ts(p.get("created_at")) for p in prds if ts(p.get("created_at"))), default=None)
    out["first_record"] = first.date().isoformat() if first else None

    task_tot = Counter()
    attempts = Counter()
    by_backend = defaultdict(lambda: [0, 0.0])
    per_day_tasks = Counter()
    per_day_prds = Counter()
    for p in prds:
        c = cls[p["id"]]
        ts_ = tasks_of(p)
        if c in ("done", "open"):
            task_tot[c + "_tasks"] += len(ts_)
            task_tot["done_tasks_all"] += sum(1 for t in ts_ if t.get("status") == "completed")
        last = None
        for t in ts_:
            at = ts(t.get("completed_at"))
            if t.get("status") == "completed" and at:
                per_day_tasks[at.astimezone().date().isoformat()] += 1
                last = max(last, at) if last else at
            for a in (t.get("attempts") or []):
                attempts["attempts"] += 1
                if a.get("outcome") == "failed":
                    attempts["failed_attempts"] += 1
                b = a.get("backend") or "unknown"
                by_backend[b][0] += 1
                by_backend[b][1] += float(a.get("duration_seconds") or 0)
        if c == "done" and last:
            per_day_prds[last.astimezone().date().isoformat()] += 1
    out["tasks"] = dict(task_tot)
    out["attempts"] = dict(attempts)
    out["worker_time_hours"] = hours(sum(v[1] for v in by_backend.values()))
    out["by_backend"] = {b: {"attempts": v[0], "hours": hours(v[1])} for b, v in
                         sorted(by_backend.items(), key=lambda kv: -kv[1][1])}
    def kind_of(p):
        return p.get("kind") or "unclassified"
    out["by_kind"] = {k: dict(Counter(cls[p["id"]] for p in prds if kind_of(p) == k))
                      for k in sorted({kind_of(p) for p in prds})}
    auto = defaultdict(Counter)
    for p in prds:
        auto[p.get("automation_id") or "(none)"][cls[p["id"]]] += 1
    out["by_automation"] = {k: dict(v) for k, v in sorted(auto.items(), key=lambda kv: -sum(kv[1].values()))[:8]}
    out["by_project"] = {"(none)": dict(Counter(cls.values()))}
    today = datetime.now().astimezone().date()
    out["last_days"] = [{"date": (today - timedelta(days=i)).isoformat(),
                         "automata_done": per_day_prds.get((today - timedelta(days=i)).isoformat(), 0),
                         "tasks_done": per_day_tasks.get((today - timedelta(days=i)).isoformat(), 0)}
                        for i in range(days)]
    try:
        import re as _re
        txt = open("CHANGELOG.md").read()
        secs = [x for x in _re.split(r"\n(?=## v)", txt) if x.startswith("## v9.")]
        cnt = Counter()
        for sec_ in secs:
            head = None
            for line in sec_.split("\n"):
                m = _re.match(r"### (\w+)", line)
                if m:
                    head = m.group(1)
                elif line.startswith("- ") and head in ("Added", "Fixed", "Changed", "Security"):
                    cnt[head] += 1
        out["changelog"] = {"release_sections": len(secs), **dict(cnt)}
    except OSError:
        out["changelog"] = None
    try:
        tags = subprocess.run(["git", "tag", "-l", "v*", "--sort=creatordate"],
                              capture_output=True, text=True).stdout.split()
        v9 = [t for t in tags if t.startswith("v9.")]
        out["releases"] = {"count": len(v9), "first": v9[0] if v9 else None,
                           "latest": v9[-1] if v9 else None, "all_tags": len(tags)}
    except OSError:
        out["releases"] = None
    return out


def show(o):
    print("OVERALL COUNTS (all records, archived finished work included)")
    c = o["by_class"]
    print(f"  records {o['records']} since {o['first_record']}: done {c.get('done', 0)}, open {c.get('open', 0)}, "
          f"abandoned {c.get('abandoned', 0)}")
    print("  by status: " + ", ".join(f"{k} {v}" for k, v in sorted(o["by_status"].items(), key=lambda kv: -kv[1])))
    t = o["tasks"]
    print(f"  tasks: {t.get('done_tasks', 0)} in done Automata, {t.get('open_tasks', 0)} in open ones "
          f"({t.get('done_tasks_all', 0)} completed overall)")
    a = o["attempts"]
    print(f"  recorded attempts {a.get('attempts', 0)} (the attempts before a task's final result, so mostly retries; "
          f"{a.get('failed_attempts', 0)} failed); worker time in them {o['worker_time_hours']} h")
    print("  by backend: " + ", ".join(f"{b} {v['attempts']} attempts {v['hours']} h" for b, v in o["by_backend"].items()))
    print("  by kind: " + "; ".join(f"{k}: " + ", ".join(f"{n} {m}" for n, m in v.items()) for k, v in o["by_kind"].items()))
    print("  by Automation (top): " + "; ".join(
        f"{k} " + "/".join(f"{n[0]}{m}" for n, m in v.items()) for k, v in o["by_automation"].items()))
    print("  by project: " + "; ".join(f"{k} " + ", ".join(f"{n} {m}" for n, m in v.items())
                                       for k, v in o["by_project"].items())
          + "  (records carry no project yet)")
    print("  last days (Automata / tasks done): " + ", ".join(
        f"{d['date'][5:]} {d['automata_done']}/{d['tasks_done']}" for d in o["last_days"]))
    cl = o.get("changelog")
    if cl:
        print(f"  changelog (9.x, {cl['release_sections']} release sections): {cl.get('Added', 0)} Added, "
              f"{cl.get('Fixed', 0)} Fixed, {cl.get('Changed', 0)} Changed, {cl.get('Security', 0)} Security entries "
              "(features shipped without an Automaton record show here, not in the Automata counts)")
    r = o.get("releases")
    if r:
        print(f"  releases in the 9.x line: {r['count']} tags, {r['first']} to {r['latest']} ({r['all_tags']} tags overall)")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--json", action="store_true")
    ap.add_argument("--days", type=int, default=7)
    ap.add_argument("--prds-file")
    a = ap.parse_args()
    o = build(load(a.prds_file), a.days)
    if a.json:
        json.dump(o, sys.stdout, indent=2)
        print()
    else:
        show(o)


if __name__ == "__main__":
    main()
