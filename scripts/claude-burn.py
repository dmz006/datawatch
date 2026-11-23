#!/usr/bin/env python3
"""Show datawatch's recent burn: tokens/min, $/min, projected $/day.

Runs two commands:
  datawatch cost report            (the grouped report with the by-model
                                    section; needs a rate configured for a
                                    USD figure)
  datawatch usage points           (live usage points read from the provider)

The by-model section reads the report's model_rows — one row per model name
with its in / out / cached tokens and its API-equivalent dollars, priced at
the model's own rate (the per-model cost entry when one is configured, else
the backend's rate). The script no longer assumes Claude and carries no
price table of its own: whatever models the daemon reports appear here, with
dollar figures computed by the daemon, so this matches the by-model section
of `datawatch cost report` exactly.

The API-equivalent dollars are a conversion, not a bill: the plan is flat,
and these show what the same tokens would cost at API rates.
"""

import json
import re
import subprocess
import sys
from datetime import datetime, timezone


def sh_json(cmd):
    """Run a command and parse its stdout as JSON."""
    out = subprocess.run(list(cmd), capture_output=True, text=True, timeout=60)
    if out.returncode != 0:
        raise RuntimeError(out.stderr.strip() or f"exit {out.returncode}")
    return json.loads(out.stdout)


def sh_text(cmd):
    out = subprocess.run(list(cmd), capture_output=True, text=True, timeout=60)
    if out.returncode != 0:
        raise RuntimeError(out.stderr.strip() or f"exit {out.returncode}")
    return out.stdout


def row_usd(r):
    """A record's dollars: the API-equivalent figure, else the estimate."""
    v = r.get("api_equivalent_usd")
    if v is None:
        v = r.get("usd_estimate")
    return float(v) if v is not None else None


def main():
    try:
        report = sh_json(["datawatch", "cost", "report", "--format", "json"])
        if not isinstance(report, dict):
            raise RuntimeError("unexpected report shape")
    except Exception as e:
        print(f"could not fetch cost report: {e}")
        sys.exit(1)

    rows = report.get("rows") or []
    model_rows = report.get("model_rows") or []
    retention_days = report.get("retention_days")
    try:
        retention_days = max(int(retention_days), 1)
    except (TypeError, ValueError):
        retention_days = 31
    minutes = retention_days * 24 * 60 + 1  # the window the figures rest on

    tot_in = sum(int(r.get("tokens_in") or 0) for r in rows)
    tot_out = sum(int(r.get("tokens_out") or 0) for r in rows)

    def sum_usd(source):
        vals = [row_usd(r) for r in source if isinstance(r, dict)]
        vals = [v for v in vals if v is not None]
        return sum(vals) if vals else None

    # Prefer the by-model figures (each priced at its own rate) for the
    # total; fall back to the grouped rows on an older daemon.
    total_usd = sum_usd(model_rows) or sum_usd(rows)

    print(f"window: last {retention_days} days (report retention)")
    print()
    print("=== burn (all models) ===")
    print(f"tokens/min:  {(tot_in + tot_out) / minutes:,.0f}")
    print(f"tokens/day:  {(tot_in + tot_out) / minutes * 1440:,.0f}")
    print(f"cache:       {sum(int(r.get('cache_read') or 0) for r in rows):,} read / "
          f"{sum(int(r.get('cache_creation') or 0) for r in rows):,} created")
    if total_usd is not None:
        print(f"$ equiv/min: ${total_usd / minutes:.6f} (API-equivalent, not a bill)")
        print(f"$ equiv/day: ${total_usd / minutes * 1440:,.2f} (API-equivalent, not a bill)")
    else:
        print("$ equiv:     no rate configured (cost.report_rate) — tokens only")

    if model_rows:
        print()
        print("=== by model ===")
        for r in sorted((r for r in model_rows if isinstance(r, dict)),
                        key=lambda x: str(x.get("model") or "")):
            name = r.get("model") or "?"
            tin = int(r.get("tokens_in") or 0)
            tout = int(r.get("tokens_out") or 0)
            u = row_usd(r)
            upm = f"${u / minutes:.6f}" if u is not None else "-"
            upday = f"${u / minutes * 1440:,.2f}" if u is not None else "-"
            print(f"{name}: {(tin + tout) / minutes:,.0f} tok/min, {upm}/min, {upday}/day, "
                  f"tokens {tin:,}/{tout:,}, cache {int(r.get('cache_read') or 0):,}/"
                  f"{int(r.get('cache_creation') or 0):,}")
        print()
        print("(by-model $ is API-equivalent at each model's own rate — the per-model")
        print("cost entry when one is configured, else the backend's; the plan is flat,")
        print("so none of this is a bill)")
    else:
        print()
        print("(no by-model section in this report — model_rows needs the daemon's")
        print("per-model breakdown)")

    # Usage points
    now = datetime.now(timezone.utc).strftime('%Y-%m-%d %H:%M UTC')
    try:
        pts_out = sh_text(["datawatch", "usage", "points"])
    except Exception as e:
        print()
        print(f"usage points: unavailable ({e})")
        return
    m = re.search(r"(\d[\d,]*)\s*points\s*used\s*/\s*(\d[\d,]*)\s*limit\s*\((\d+)%\)\s*of\s*([\d,]+)\s*pts/mo", pts_out)
    if m:
        used, limit, pct, monthly = m.group(1), m.group(2), m.group(3), m.group(4)
        print()
        print(f"=== usage points (live, {now}) ===")
        print(f"points used: {used} / {limit} ({pct}%) of {monthly} pts/mo")
        if total_usd is not None:
            implied = total_usd / int(used.replace(',', '')) if int(used.replace(',', '')) else None
            if implied is not None:
                print(f"implied: ${implied:.2f} per point; {int(monthly.replace(',', ''))} pts x ${implied:.2f} = "
                      f"${int(monthly.replace(',', '')) * implied:,.0f}/mo API-equivalent")
        print()
        print("(points = provider-side quota, $ = local API-equivalent estimate)")


if __name__ == "__main__":
    main()
