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
cause, stall and similar words mean bug). Cancelled PRDs whose tasks all completed count as done (merged work whose
record was closed at merge); other cancelled, rejected and archived PRDs are
replaced or abandoned records and are not counted.

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
        has_prd = bool(re.search(r"PRD `[0-9a-f]{8}`|`[0-9a-f]{8}`", " ".join(cells)))
        rows.append((m.group(1), item[:70], status[:60], has_prd))
    return rows


def main():
    want_json = "--json" in sys.argv
    since = SINCE
    if "--since" in sys.argv:
        since = sys.argv[sys.argv.index("--since") + 1]
    since_dt = datetime.fromisoformat(since).replace(tzinfo=timezone.utc)
    now = datetime.now(timezone.utc)

    prds = []
    for p in load_prds():
        if p["status"] in SKIP_STATUS:
            # A cancelled record whose tasks all completed is merged work whose
            # record was closed at merge time: count it as done. Anything else
            # cancelled, rejected or archived is a replaced or abandoned record.
            ts = [t for s in p.get("stories", []) for t in s.get("tasks", [])]
            if p["status"] == "cancelled" and ts and all(t["status"] == "completed" for t in ts):
                p = dict(p, status="completed")
            else:
                continue
        prds.append(p)
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
                # Time the PRD was actually being worked: first task start to
                # last task completion. approved_at -> updated_at is wrong: it
                # includes weeks of waiting and every later edit or cancel.
                starts = [parse_ts(t.get("started_at")) for sy in p.get("stories", []) for t in sy.get("tasks", [])]
                ends = [parse_ts(t.get("completed_at")) for sy in p.get("stories", []) for t in sy.get("tasks", [])]
                starts = [x for x in starts if x]
                ends = [x for x in ends if x]
                if starts and ends and max(ends) > min(starts):
                    s["prd_wall_h"].append((max(ends) - min(starts)).total_seconds() / 3600.0)
        return s

    st = {k: stats(v) for k, v in groups.items()}
    total_done = st["feature"]["done"] + st["bug"]["done"]
    first = min((parse_ts(p.get("created_at")) for p in prds if parse_ts(p.get("created_at"))), default=now)
    elapsed_days = max((now - max(first, since_dt)).total_seconds() / 86400.0, 0.1)

    def last_done(p):
        ends = [parse_ts(t.get("completed_at")) for sy in p.get("stories", []) for t in sy.get("tasks", [])]
        ends = [x for x in ends if x]
        return max(ends) if ends else None
    done_ps = [p for p in prds if p["status"] in DONE_STATUS]
    cut24 = now.timestamp() - 86400
    cut72 = now.timestamp() - 3 * 86400
    last24 = sum(1 for p in done_ps if last_done(p) and last_done(p).timestamp() >= cut24)
    last72 = sum(1 for p in done_ps if last_done(p) and last_done(p).timestamp() >= cut72)
    throughput = last72 / 3.0 if last72 else 0.0

    open_rows = registry_open_rows()
    # Remaining work = (1) PRDs that already exist and are not done, with the
    # tasks they still have, plus (2) work that has no PRD yet: registry rows
    # with no PRD id, and the PRDs that multi-phase rows will expand into.
    # Existing PRDs are never counted a second time through their registry row.
    existing_left = [p for p in prds if p["status"] not in DONE_STATUS]
    existing_prds_left = len(existing_left)
    existing_tasks_left = sum(
        1 for p in existing_left for story in p.get("stories", [])
        for t in story.get("tasks", []) if t["status"] != "completed"
    )
    undecomposed = sum(
        1 for p in existing_left
        if not any(story.get("tasks") for story in p.get("stories", []))
    )
    avg_tasks = (
        (st["feature"]["tasks"] + st["bug"]["tasks"]) / max(len(prds), 1)
    )
    unfiled_rows = [r for r in open_rows if not r[3]]
    expand = 0
    for rid, item, _, _ in open_rows:
        if "BL405" in item:
            expand += 10
        if item.startswith("Security remediation"):
            expand += 3
    new_prds = len(unfiled_rows) + expand
    remaining_prds = existing_prds_left + new_prds
    est_tasks_new = (new_prds + undecomposed) * avg_tasks
    remaining_tasks = existing_tasks_left + est_tasks_new
    in_flight = sum(s["running"] for s in st.values())
    walls = sorted(h for s in st.values() for h in s["prd_wall_h"])
    avg_wall_h = walls[len(walls) // 2] if walls else None  # median
    eta_days = (remaining_prds / throughput) if throughput else None
    total_work_h = st["feature"]["work_h"] + st["bug"]["work_h"]
    done_tasks_n = st["feature"]["tasks_done"] + st["bug"]["tasks_done"]
    h_per_task = total_work_h / done_tasks_n if done_tasks_n else None
    worker_h_per_day = total_work_h / elapsed_days
    utilization = worker_h_per_day / (LANES * 24.0)
    work_left_h = remaining_tasks * h_per_task if h_per_task else None
    eta_lane_days = (work_left_h / worker_h_per_day) if (work_left_h and worker_h_per_day) else None

    result = dict(
        since=since,
        elapsed_days=round(elapsed_days, 2),
        groups=st,
        completed_prds=total_done,
        running_prds=in_flight,
        existing_prds_left=existing_prds_left,
        existing_tasks_left=existing_tasks_left,
        new_prds_not_yet_filed=new_prds,
        remaining_prds=remaining_prds,
        remaining_tasks_est=round(remaining_tasks),
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
        "work", "med/PRD", "avg/task"))
    for g in ("feature", "bug"):
        s = st[g]
        ws = sorted(s["prd_wall_h"]); aw = ws[len(ws) // 2] if ws else None
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
    print("LEFT  existing PRDs not done: %d (%d running, %d waiting or blocked) with %d tasks still open; "
          "%d of them are not decomposed yet" % (
              existing_prds_left, in_flight, existing_prds_left - in_flight,
              existing_tasks_left, undecomposed))
    print("      not yet filed: %d registry rows with no PRD + %d PRDs from multi-phase rows = %d new PRDs"
          % (len(unfiled_rows), expand, new_prds))
    print("      total left: about %d PRDs and about %d tasks (%d existing + %.0f estimated at %.1f tasks/PRD)" % (
        remaining_prds, remaining_tasks, existing_tasks_left, est_tasks_new, avg_tasks))
    print("ETA   PRD rate %.1f PRDs/day (last 3 days, by last task completed) -> about %s" % (
        throughput, "-" if eta_days is None else "%.1f days" % eta_days))
    print("      worker-hours: %.2f h/task x %d tasks = %s of work; lanes did %.1f worker-h/day = %.0f%% of %d lanes -> about %s at today's utilization" % (
        h_per_task or 0, remaining_tasks, "-" if work_left_h is None else "%.0f h" % work_left_h,
        worker_h_per_day, utilization * 100, LANES,
        "-" if eta_lane_days is None else "%.1f days" % eta_lane_days))
    # Task-based estimate: tasks completed per day over the whole period and
    # over the last 24 hours, against the estimated tasks left.
    cutoff = datetime.fromtimestamp(now.timestamp() - 86400, timezone.utc)
    done_tasks = [t for p in prds for story in p.get("stories", []) for t in story.get("tasks", [])
                  if t["status"] == "completed"]
    last24_tasks = sum(1 for t in done_tasks if (parse_ts(t.get("completed_at")) or now) >= cutoff)
    rate_all = len(done_tasks) / elapsed_days
    print("      task math: %d tasks left at %.0f tasks/day overall -> %.1f days; at %d tasks in the last 24 h -> %s" % (
        remaining_tasks, rate_all, remaining_tasks / rate_all if rate_all else 0,
        last24_tasks, "-" if not last24_tasks else "%.1f days" % (remaining_tasks / last24_tasks)))
    print("      (estimate only; a range, not a promise. Many blocked/waiting PRDs are old and may never run.)")
    print("UNFILED ROWS (first 14 of %d): %s" % (
        len(unfiled_rows),
        "; ".join("%s %s" % (r[0], r[1][:30]) for r in unfiled_rows[:14])))


if __name__ == "__main__":
    main()
