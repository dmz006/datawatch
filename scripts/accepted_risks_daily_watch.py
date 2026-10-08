#!/usr/bin/env python3
"""Daily watch report for security/accepted-risks.yml (GH#197's shared
standard). Sections, in this order (per the shared spec -- 24h section
comes first):

  1. Added or renewed in the last 24h
  2. Re-trace needed (accepted `version` differs from what a fresh,
     unsuppressed scan found installed)
  3. Past expires, or due within 14 days
  4. Stable fixes now published (left to image-refresh.yaml's existing
     fresh-rescan-union diff + scripts/apply_stale_risk_removal.py --
     this script does not duplicate that)

Input: the registry, plus one or more fresh Trivy JSON scan files (no
--ignorefile) to source live installed-version data from. Usage:

  python3 scripts/accepted_risks_daily_watch.py security/accepted-risks.yml /tmp/scan-*.json
"""
import datetime
import json
import sys

try:
    import yaml
except ImportError:
    sys.exit("PyYAML is required: pip install pyyaml")


def load_live_versions(scan_paths):
    """package name -> installed version, from any of the fresh scans."""
    versions = {}
    for path in scan_paths:
        try:
            with open(path) as f:
                data = json.load(f)
        except (OSError, json.JSONDecodeError):
            continue
        for res in data.get("Results") or []:
            for v in res.get("Vulnerabilities") or []:
                pkg = v.get("PkgName")
                ver = v.get("InstalledVersion")
                if pkg and ver:
                    versions[pkg] = ver
    return versions


def cve_id(entry):
    return entry["id"].split("/", 1)[1] if "/" in entry["id"] else entry["id"]


def normalize_debian_version(v):
    """Strip a Debian epoch prefix ("1:2.38.1-5" -> "2.38.1-5") so an
    epoch-notation inconsistency between two scans of the same real
    version doesn't look like a re-trace-worthy version change."""
    return v.split(":", 1)[1] if v and ":" in v and v.split(":", 1)[0].isdigit() else v


def main(argv):
    if not argv:
        sys.exit(__doc__)
    registry_path = argv[0]
    scan_paths = argv[1:]

    with open(registry_path) as f:
        entries = (yaml.safe_load(f) or {}).get("entries") or []

    live_versions = load_live_versions(scan_paths)
    today = datetime.date.today()

    added_24h = []
    renewed_24h = []
    retrace = []
    due_soon = []
    past_expiry = []

    for e in entries:
        added = datetime.date.fromisoformat(e["added"])
        first_added = datetime.date.fromisoformat(e["first_added"])
        expires = datetime.date.fromisoformat(e["expires"])

        if (today - added).days <= 1:
            if added == first_added:
                added_24h.append(e)
            else:
                renewed_24h.append(e)

        # Re-trace: the FIRST package name in a comma-separated list is the
        # one most entries key their version field to; check any listed
        # package name that we have live data for.
        pkg_names = [p.strip() for p in e["package"].split(",")]
        for pkg in pkg_names:
            pkg_base = pkg.split()[0] if pkg else ""
            if pkg_base in live_versions and normalize_debian_version(live_versions[pkg_base]) != normalize_debian_version(e.get("version")):
                retrace.append((e, pkg_base, e.get("version"), live_versions[pkg_base]))
                break

        if expires < today:
            past_expiry.append(e)
        elif (expires - today).days <= 14:
            due_soon.append(e)

    lines = ["# security/accepted-risks.yml daily watch", ""]

    lines.append(f"## Added or renewed in the last 24h ({len(added_24h) + len(renewed_24h)})")
    lines.append("")
    if not added_24h and not renewed_24h:
        lines.append("None.")
    for e in added_24h:
        lines.append(f"- **new** `{cve_id(e)}` ({e['package']}) -- validated by {e['validated_by']}")
    for e in renewed_24h:
        lines.append(f"- **renewed** `{cve_id(e)}` ({e['package']}), first accepted {e['first_added']} -- validated by {e['validated_by']}")
    lines.append("")

    lines.append(f"## Re-trace needed -- accepted version no longer matches what's installed ({len(retrace)})")
    lines.append("")
    if not retrace:
        lines.append("None.")
    for e, pkg, old_v, new_v in retrace:
        lines.append(f"- `{cve_id(e)}` ({pkg}): accepted at `{old_v}`, now `{new_v}` -- re-trace regardless of `expires`.")
    lines.append("")

    lines.append(f"## Past expires, or due within 14 days ({len(past_expiry) + len(due_soon)})")
    lines.append("")
    if not past_expiry and not due_soon:
        lines.append("None.")
    for e in sorted(past_expiry, key=lambda e: e["expires"]):
        lines.append(f"- **EXPIRED** `{cve_id(e)}` -- expired {e['expires']} (added {e['added']}, traced={e['impact']['traced']})")
    for e in sorted(due_soon, key=lambda e: e["expires"]):
        lines.append(f"- `{cve_id(e)}` -- due {e['expires']} (added {e['added']}, traced={e['impact']['traced']})")
    lines.append("")

    lines.append(f"_{len(entries)} entries total, generated {today.isoformat()}. Stable-fix detection and automatic removal PRs are image-refresh.yaml's recheck-ignored-cves job, not this report._")

    print("\n".join(lines))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
