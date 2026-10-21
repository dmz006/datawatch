#!/usr/bin/env python3
"""scripts/v10-status.py — v10.0.0 progress report built from live PRD data.

Reads the Automata (PRD) list from the running daemon (`datawatch autonomous
prd-list`) and the plans registry (docs/plans/README.md) and prints, for the
supervisor tick:

  * PRDs split into features and bugs: total, done, running, waiting, blocked
  * tasks for each: total, done, left
  * time spent: calendar time since the roadmap lock, summed worker time,
    average time per PRD and per task
  * what is left in the registry for 10.0.0 (open rows) and an estimate of
    time to completion from the measured throughput

Classification is a heuristic on title and spec (fix, flaky, defect, root
cause, stall and similar words mean bug). Cancelled, rejected and archived
PRDs are records of replaced or merged work and are not counted.

Usage: python3 scripts/v10-status.py [--json] [--since 2026-10-10]
No third-party packages; stdlib only.
"""
import json
import re
import subprocess
import sys
from datetime import datetime, timezone

SINCE = "2026-10-10"  # roadmap lock date (docs/plans/README.md)
LANES = 5  # two GPU lanes + three Claude lanes

BUG_RE = re.compile(
    r"\b(fix|flak|defect|bug|root.cause|stall|regress|race|leak|must not|"
    r"no longer|crash|broken|fails?|failure|blocker|suppress|cve|trivy|"
    r"deterministic|isolate|refuse|stuck)\b",
    re.I,
)
SKIP_STATUS = {"cancelled", "rejected", "archived"}
DONE_STATUS = {"completed"}
RUN_STATUS = {"running", "decomposing", "planning"}
OPEN_ROW_RE = re.compile(
    r"not started|not filed|draft|merge pending|running|approved|blocked|"
    r"queued|planning|failed|in progress|decomposed|needs design",
    re.I,
)
DONE_ROW_RE = re.compile(
    r"\*\*done\*\*|\bdone\b|shipped|superseded|rejected|closed|cancelled|"
    r"implemented|completed(?!, merge pending)",
    re.I,
)
POST10_RE = re.compile(r"post-10\.0\.0|deferred|after 10\.0\.0|post 10\.0", re.I)


def parse_ts(s):
    if not s:
        return None
    try:
        return datetime.fromisoformat(s.replace("Z", "+00:00"))
    except ValueError:
        return None


def load_prds():
    out = subprocess.run(
        ["datawatch", "autonomous", "prd-list"], capture_output=True, text=True
    )
    d = json.loads(out.stdout)
    return d.get("prds", d)


def is_bug(p):
    text = (p.get("title") or "") + " " + (p.get("spec") or "")[:240]
    return bool(BUG_RE.search(text))


def fmt_h(hours):
    if hours is None:
        return "-"
    if hours >= 48:
        return "%.1f d" % (hours / 24.0)
    return "%.1f h" % hours


def registry_open_rows(path="docs/plans/README.md"):
    rows = []
    for line in open(path).read().split("\n"):
        m = re.match(r"\| (\d+[a-z]?) \| (.*)", line)
        if not m:
            continue
        cells = [c.strip() for c in line.strip().strip("|").split("|")]
        if len(cells) < 3:
            continue
        item, status = cells[1], cells[2]
        if DONE_ROW_RE.search(status[:140]) and "merge pending" not in status:
            continue
        if not OPEN_ROW_RE.search(status[:200]):
            continue
        if POST10_RE.search(" ".join(cells)):
            continue
        rows.append((m.group(1), item[:70], status[:60]))
    return rows


def main():
    want_json = "--json" in sys.argv
    since = SINCE
    if "--since" in sys.argv:
        since = sys.argv[sys.argv.index("--since") + 1]
    since_dt = datetime.fromisoformat(since).replace(tzinfo=timezone.utc)
    now = datetime.now(timezone.utc)

    prds = [p for p in load_prds() if p["status"] not in SKIP_STATUS]
    prds = [
        p
        for p in prds
        if (parse_ts(p.get("created_at")) or now) >= since_dt
        or p["status"] not in DONE_STATUS
    ]

    groups = {"feature": [], "bug": []}
    for p in prds:
        groups["bug" if is_bug(p) else "feature"].append(p)

    def stats(ps):
        s = dict(prds=len(ps), done=0, running=0, waiting=0, blocked=0,
                 tasks=0, tasks_done=0, work_h=0.0, prd_wall_h=[], task_h=[])
        for p in ps:
            st = p["status"]
            if st in DONE_STATUS:
                s["done"] += 1
            elif st in RUN_STATUS:
                s["running"] += 1
            elif st in ("blocked", "failed"):
                s["blocked"] += 1
            else:
                s["waiting"] += 1
            for story in p.get("stories", []):
                for t in story.get("tasks", []):
                    s["tasks"] += 1
                    if t["status"] == "completed":
                        s["tasks_done"] += 1
                        a, b = parse_ts(t.get("started_at")), parse_ts(t.get("completed_at"))
                        if a and b and b > a:
                            h = (b - a).total_seconds() / 3600.0
                            s["work_h"] += h
                            s["task_h"].append(h)
            if st in DONE_STATUS:
                a = parse_ts(p.get("approved_at")) or parse_ts(p.get("created_at"))
                b = parse_ts(p.get("updated_at"))
                if a and b and b > a:
                    s["prd_wall_h"].append((b - a).total_seconds() / 3600.0)
        return s

    st = {k: stats(v) for k, v in groups.items()}
    total_done = st["feature"]["done"] + st["bug"]["done"]
    first = min((parse_ts(p.get("created_at")) for p in prds if parse_ts(p.get("created_at"))), default=now)
    elapsed_days = max((now - max(first, since_dt)).total_seconds() / 86400.0, 0.1)

    done_ps = [p for p in prds if p["status"] in DONE_STATUS]
    last24 = sum(
        1 for p in done_ps
        if (parse_ts(p.get("updated_at")) or now) >= datetime.fromtimestamp(now.timestamp() - 86400, timezone.utc)
    )
    throughput = max(last24, total_done / elapsed_days if elapsed_days else 0)

    open_rows = registry_open_rows()
    # Remaining PRDs: open registry rows (each about one PRD; phase rows are
    # one PRD per phase) plus in-flight and waiting PRDs not yet done.
    in_flight = sum(s["running"] + s["waiting"] + s["blocked"] for s in st.values())
    avg_tasks = (
        (st["feature"]["tasks"] + st["bug"]["tasks"]) / max(len(prds), 1)
    )
    # BL405 phases 0-10 and the four security items expand into several PRDs.
    expand = 0
    for rid, item, _ in open_rows:
        if "BL405" in item:
            expand += 10
        if item.startswith("Security remediation"):
            expand += 3
    remaining_prds = len(open_rows) + expand
    avg_wall = [h for s in st.values() for h in s["prd_wall_h"]]
    avg_wall_h = sum(avg_wall) / len(avg_wall) if avg_wall else None
    eta_days = (remaining_prds / throughput) if throughput else None
    eta_lane_days = (
        remaining_prds * avg_wall_h / LANES / 24.0 if avg_wall_h else None
    )

    result = dict(
        since=since,
        elapsed_days=round(elapsed_days, 2),
        groups=st,
        completed_prds=total_done,
        in_flight_prds=in_flight,
        open_registry_rows=len(open_rows),
        expanded_prds=expand,
        remaining_prds=remaining_prds,
        throughput_per_day=round(throughput, 1),
        avg_prd_wall_h=avg_wall_h,
        avg_tasks_per_prd=round(avg_tasks, 1),
        eta_days_by_throughput=None if eta_days is None else round(eta_days, 1),
        eta_days_by_lane_math=None if eta_lane_days is None else round(eta_lane_days, 1),
    )
    if want_json:
        for g in result["groups"].values():
            g["prd_wall_h"] = len(g["prd_wall_h"])
            g["task_h"] = len(g["task_h"])
        print(json.dumps(result, indent=1))
        return

    print("v10.0.0 STATUS  (since %s, %.1f days elapsed, %d lanes)" % (since, elapsed_days, LANES))
    print("%-8s %5s %5s %5s %6s %7s | %6s %6s %5s | %9s %9s %9s" % (
        "", "PRDs", "done", "run", "wait", "blocked", "tasks", "done", "left",
        "work", "avg/PRD", "avg/task"))
    for g in ("feature", "bug"):
        s = st[g]
        aw = sum(s["prd_wall_h"]) / len(s["prd_wall_h"]) if s["prd_wall_h"] else None
        at = sum(s["task_h"]) / len(s["task_h"]) if s["task_h"] else None
        print("%-8s %5d %5d %5d %6d %7d | %6d %6d %5d | %9s %9s %9s" % (
            g, s["prds"], s["done"], s["running"], s["waiting"], s["blocked"],
            s["tasks"], s["tasks_done"], s["tasks"] - s["tasks_done"],
            fmt_h(s["work_h"]), fmt_h(aw), fmt_h(at)))
    tp = st["feature"]["prds"] + st["bug"]["prds"]
    tt = st["feature"]["tasks"] + st["bug"]["tasks"]
    td = st["feature"]["tasks_done"] + st["bug"]["tasks_done"]
    print("%-8s %5d %5d %5s %6s %7s | %6d %6d %5d | %9s" % (
        "total", tp, total_done, "", "", "", tt, td, tt - td,
        fmt_h(st["feature"]["work_h"] + st["bug"]["work_h"])))
    print("")
    print("LEFT  open registry rows: %d (+%d PRDs from phase/multi-item rows) = about %d more PRDs, "
          "%d already in flight; about %.0f more tasks at %.1f tasks/PRD" % (
              len(open_rows), expand, remaining_prds, in_flight,
              remaining_prds * avg_tasks, avg_tasks))
    print("ETA   throughput %.1f PRDs/day -> about %s; lane math (avg %s/PRD over %d lanes) -> about %s" % (
        throughput,
        "-" if eta_days is None else "%.1f days" % eta_days,
        fmt_h(avg_wall_h), LANES,
        "-" if eta_lane_days is None else "%.1f days" % eta_lane_days))
    print("      (estimate only: open rows are counted as one PRD each; BL405 counts 10 more, security 3 more)")
    print("OPEN ROWS (first 14 of %d): %s" % (
        len(open_rows),
        "; ".join("%s %s" % (r[0], r[1][:30]) for r in open_rows[:14])))


if __name__ == "__main__":
    main()
