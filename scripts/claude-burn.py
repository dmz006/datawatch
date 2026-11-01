#!/usr/bin/env python3
"""Claude spend since a point in time, from the local session transcripts.

Usage: python3 scripts/claude-burn.py [--since "YYYY-MM-DD HH:MM"] [--per-point 21]

Sums the usage recorded in the Claude session transcripts (assistant turns, de-duplicated
by request id) as a cost-equivalent in dollars (per million tokens: haiku 1/5, sonnet 3/15,
opus 15/75 for input/output; cache writes 1.25x input, cache reads 0.1x input), then prints
the total, the hourly rate, points at --per-point dollars per usage point, and the split
between supervisor, workers and verifiers. The dollar figure is an estimate used only to
compare against the weekly pace line, not a bill. The per-point value is calibrated from
the operator's readings and should be re-checked when a new reading arrives.
"""
import argparse, collections, datetime as dt, glob, json, os

RATE = {"haiku": (1, 5), "sonnet": (3, 15), "opus": (15, 75), "fable": (15, 75)}


def cost(model, u):
    key = next((k for k in RATE if k in (model or "").lower()), "sonnet")
    i, o = RATE[key]
    return (u.get("input_tokens", 0) * i + u.get("output_tokens", 0) * o
            + u.get("cache_creation_input_tokens", 0) * i * 1.25
            + u.get("cache_read_input_tokens", 0) * i * 0.1) / 1e6


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--since", default=None)
    ap.add_argument("--per-point", type=float, default=21.0)
    a = ap.parse_args()
    now = dt.datetime.now()
    since = dt.datetime.strptime(a.since, "%Y-%m-%d %H:%M") if a.since else now - dt.timedelta(hours=1)
    root = os.path.expanduser("~/.claude/projects")
    split, seen = collections.defaultdict(float), set()
    for f in glob.glob(root + "/*/*.jsonl"):
        if os.path.getmtime(f) < since.timestamp():
            continue
        folder = os.path.basename(os.path.dirname(f))
        for line in open(f, errors="ignore"):
            try:
                d = json.loads(line)
            except ValueError:
                continue
            m = d.get("message") or {}
            u = m.get("usage")
            if not u or d.get("type") != "assistant":
                continue
            rid = d.get("requestId") or m.get("id")
            if rid in seen:
                continue
            seen.add(rid)
            t = dt.datetime.fromisoformat(d["timestamp"].replace("Z", "+00:00")).astimezone().replace(tzinfo=None)
            if t < since:
                continue
            haiku = "haiku" in (m.get("model") or "")
            kind = "workers/verifiers" if "prd-worktrees" in folder else "supervisor/other"
            split[kind + (" (haiku)" if haiku else "")] += cost(m.get("model"), u)
    total = sum(split.values())
    hours = max((now - since).total_seconds() / 3600, 0.01)
    print("since %s: $%.0f, %.1f points at $%.0f/point, $%.1f/hour = %.2f points/hour"
          % (since.strftime("%a %H:%M"), total, total / a.per_point, a.per_point, total / hours, total / hours / a.per_point))
    print("allowed pace: 14.3 points/day = 0.60 points/hour = about $12.5/hour at $21/point")
    for k, v in sorted(split.items(), key=lambda x: -x[1]):
        print("  %-28s $%6.0f  %4.1f%%" % (k, v, 100 * v / total if total else 0))


if __name__ == "__main__":
    main()
