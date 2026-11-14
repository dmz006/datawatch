#!/usr/bin/env python3
"""Claude usage since a point in time, in tokens, from the local session transcripts.

Usage: python3 scripts/claude-burn.py [--since "YYYY-MM-DD HH:MM"]

The Claude allowance is token based. This sums the usage recorded in the session transcripts
(assistant turns, de-duplicated by request id) by kind: uncached input, output, cache writes
and cache reads, split between workers/verifiers (sessions in task worktrees) and the
supervisor/other sessions. Points are estimated with coefficients fitted to the operator's
usage readings of 2026-10-25 to 2026-10-31 (16 intervals): 0.373 points per million cache-write
tokens and 0.0069 points per million cache-read tokens (output tokens correlate with cache
writes and add nothing once those are in; the fit error is about plus or minus 1.5 points per
interval). Refit when new readings arrive. The result is an estimate to compare with the
weekly pace line (14.3 points a day, 0.6 points an hour), not an exact meter.
"""
import argparse, collections, datetime as dt, glob, json, os

CW, CR = 0.373, 0.0069  # points per million tokens


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--since", default=None)
    a = ap.parse_args()
    now = dt.datetime.now()
    since = dt.datetime.strptime(a.since, "%Y-%m-%d %H:%M") if a.since else now - dt.timedelta(hours=1)
    root = os.path.expanduser("~/.claude/projects")
    tok = collections.defaultdict(lambda: [0, 0, 0, 0])
    seen = set()
    # Top-level session files plus the subagent transcripts that Claude Code keeps
    # under <project>/<session>/subagents/: helper agents (merge, repair, audit) used
    # real tokens that the first version of this script did not count.
    files = glob.glob(root + "/*/*.jsonl") + glob.glob(root + "/*/*/subagents/*.jsonl")
    for f in files:
        if os.path.getmtime(f) < since.timestamp():
            continue
        if "/subagents/" in f:
            kind = "helper agents (subagents)"
        else:
            kind = "workers/verifiers" if "prd-worktrees" in os.path.basename(os.path.dirname(f)) else "supervisor/other"
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
            x = tok[kind]
            x[0] += u.get("input_tokens", 0)
            x[1] += u.get("output_tokens", 0)
            x[2] += u.get("cache_creation_input_tokens", 0)
            x[3] += u.get("cache_read_input_tokens", 0)
    pts = {k: v[2] / 1e6 * CW + v[3] / 1e6 * CR for k, v in tok.items()}
    total = sum(pts.values())
    hours = max((now - since).total_seconds() / 3600, 0.01)
    print("since %s: about %.1f points, %.2f points/hour (allowed pace 0.60/hour, 14.3/day)" % (since.strftime("%a %H:%M"), total, total / hours))
    for k, v in sorted(tok.items(), key=lambda x: -pts[x[0]]):
        print("  %-18s %5.1f pts  in %.1fk  out %.0fk  cache-write %.1fM  cache-read %.0fM"
              % (k, pts[k], v[0] / 1e3, v[1] / 1e3, v[2] / 1e6, v[3] / 1e6))


if __name__ == "__main__":
    main()
