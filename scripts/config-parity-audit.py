#!/usr/bin/env python3
"""Config-parity audit: checks every config key against the five surfaces.

Surfaces: web, get, put, configure, cli (the yaml surface is the struct
itself, so it is always present). Stdlib only; runs under ``python3 -I``.

Exit codes: 0 every gap exempt, 1 unexempted gap, 2 usage/parse error.
"""

import argparse
import importlib.util
import json
import os
import shutil
import subprocess
import sys

SURFACES = ("web", "get", "put", "configure", "cli")
HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
DEFAULT_EXEMPTIONS = os.path.join("scripts", "config-parity-exemptions.txt")


class AuditError(Exception):
    pass


def load_surfaces_module():
    path = os.path.join(HERE, "config_parity_surfaces.py")
    spec = importlib.util.spec_from_file_location("config_parity_surfaces", path)
    if spec is None or spec.loader is None:
        raise AuditError("cannot load " + path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def list_keys():
    go = shutil.which("go")
    if not go:
        raise AuditError("go not found on PATH; install Go to list config keys")
    try:
        p = subprocess.run([go, "run", "./scripts/config-parity-keys"], cwd=ROOT,
                           stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                           universal_newlines=True)
    except OSError as e:
        raise AuditError("cannot run go: %s" % e)
    if p.returncode != 0:
        raise AuditError("go run ./scripts/config-parity-keys failed (exit %d): %s"
                         % (p.returncode, p.stderr.strip()))
    keys = []
    seen = set()
    for line in p.stdout.splitlines():
        k = line.strip()
        if k and k not in seen:
            seen.add(k)
            keys.append(k)
    return keys


def parse_exemptions(path):
    """Return list of (pattern, surfaces_set, reason, lineno)."""
    entries = []
    try:
        with open(path, "r", encoding="utf-8") as f:
            lines = f.read().splitlines()
    except OSError as e:
        raise AuditError("cannot read exemptions file %s: %s" % (path, e))
    for n, raw in enumerate(lines, 1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        parts = [p.strip() for p in line.split("|", 2)]
        if len(parts) != 3:
            raise AuditError("%s:%d: expected 'key | surfaces | reason'" % (path, n))
        key, surfs, reason = parts
        if not key:
            raise AuditError("%s:%d: empty key" % (path, n))
        if not reason:
            raise AuditError("%s:%d: exemption for %r has an empty reason" % (path, n, key))
        if surfs == "*":
            sset = set(SURFACES)
        else:
            sset = set(s.strip() for s in surfs.split(",") if s.strip())
            bad = sset - set(SURFACES)
            if not sset or bad:
                raise AuditError("%s:%d: bad surfaces %r (use %s or *)"
                                 % (path, n, surfs, ",".join(SURFACES)))
        entries.append((key, sset, reason, n))
    return entries


def exempt_match(pattern, key):
    if pattern.endswith(".*"):
        prefix = pattern[:-2]
        return key == prefix or key.startswith(prefix + ".")
    return key == pattern


def main(argv=None):
    ap = argparse.ArgumentParser(description="Audit config keys across the five config surfaces.")
    ap.add_argument("--all", action="store_true", help="print every key, not only keys with a gap")
    ap.add_argument("--no-exemptions", action="store_true", help="ignore the exemptions file")
    ap.add_argument("--json", action="store_true", help="machine output of gaps per surface")
    ap.add_argument("--emit-exemptions", action="store_true",
                    help="print exemption lines for every current gap (uses --reason)")
    ap.add_argument("--reason", default="", help="reason text for --emit-exemptions")
    ap.add_argument("--exemptions", default=DEFAULT_EXEMPTIONS, metavar="PATH",
                    help="exemptions file (default: %(default)s)")
    try:
        args = ap.parse_args(argv)
    except SystemExit as e:
        return 0 if e.code in (0, None) else 2

    try:
        if args.emit_exemptions and not args.reason.strip():
            raise AuditError("--emit-exemptions requires a non-empty --reason")
        exemptions = []
        if not args.no_exemptions:
            path = args.exemptions
            if not os.path.isabs(path):
                path = os.path.join(ROOT, path)
            exemptions = parse_exemptions(path)
        mod = load_surfaces_module()
        keys = list_keys()
        sets = {
            "web": mod.web_settings_keys(ROOT),
            "get": mod.api_get_keys(ROOT),
            "put": mod.api_put_keys(ROOT),
            "configure": mod.router_configure_keys(ROOT),
            "cli": mod.cli_config_set_keys(ROOT),
        }
    except AuditError as e:
        print("config-parity-audit: error: %s" % e, file=sys.stderr)
        return 2
    except Exception as e:  # reader failure
        print("config-parity-audit: error: %s: %s" % (type(e).__name__, e), file=sys.stderr)
        return 2

    rows = []  # (key, {surface: status})
    gaps = {s: [] for s in SURFACES}
    total_gaps = 0
    total_exempt = 0
    used = set()
    for key in keys:
        status = {}
        for s in SURFACES:
            if mod.covers(sets[s], key):
                status[s] = "ok"
                continue
            ex = None
            for i, (pat, sset, _r, _n) in enumerate(exemptions):
                if s in sset and exempt_match(pat, key):
                    ex = i
                    break
            if ex is not None:
                status[s] = "exempt"
                total_exempt += 1
                used.add(ex)
            else:
                status[s] = "MISSING"
                total_gaps += 1
                gaps[s].append(key)
        rows.append((key, status))

    # Stale exemptions: exempted on a surface where the key is present.
    stale = []
    for pat, sset, _r, n in exemptions:
        for s in sorted(sset):
            for key in keys:
                if exempt_match(pat, key) and mod.covers(sets[s], key):
                    stale.append("stale exemption (line %d): %r on %s, but %s is present"
                                 % (n, pat, s, key))
    for w in stale:
        print("warning: " + w, file=sys.stderr)

    if args.emit_exemptions:
        for key, status in rows:
            miss = [s for s in SURFACES if status[s] == "MISSING"]
            if miss:
                print("%s | %s | %s" % (key, ",".join(miss), args.reason.strip()))
        return 0

    if args.json:
        print(json.dumps({"total_keys": len(keys), "total_gaps": total_gaps,
                          "total_exempt": total_exempt, "gaps": gaps}, indent=2, sort_keys=True))
    else:
        shown = [(k, st) for k, st in rows
                 if args.all or any(v == "MISSING" for v in st.values())]
        w = max([len("key")] + [len(k) for k, _ in shown])
        cw = {s: max(len(s), 7) for s in SURFACES}
        print("  ".join(["key".ljust(w)] + [s.ljust(cw[s]) for s in SURFACES]).rstrip())
        for k, st in shown:
            print("  ".join([k.ljust(w)] + [st[s].ljust(cw[s]) for s in SURFACES]).rstrip())
        print("total keys: %d, total gaps: %d, total exempt: %d"
              % (len(keys), total_gaps, total_exempt))
    return 1 if total_gaps else 0


if __name__ == "__main__":
    sys.exit(main())
