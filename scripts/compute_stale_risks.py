#!/usr/bin/env python3
"""Decide which security/accepted-risks.yml entries are genuinely stale
(a fix is now installed), anchored to the INSTALLED PACKAGE VERSION rather
than whether the suppressed CVE ID still appears in a fresh scan.

Why: Trivy's vulnerability database gets rebuilt multiple times a day and
can relabel which CVE ID applies to an unchanged package/version (confirmed
2026-10-08 -- zlib1g 1:1.2.13.dfsg-1 reported as CVE-2023-45853 in one scan
and CVE-2026-27171/CVE-2026-85091 a few hours later, Status: affected both
times, no FixedVersion, package identical). The old ID-presence diff (`comm
-23 suppressed.txt all-vulnerable.txt`) treated that relabeling as "fixed
upstream" and would have deleted a still-valid suppression (caught before
merge as PR #199). A real fix always bumps the installed version; database
churn never does. This script only calls an entry stale when the package it
names is no longer installed at the exact suppressed version in ANY image
it applies to that we have fresh scan data for this run.

Requires the fresh scans to be Trivy `--list-all-pkgs` JSON, so a package
with zero current findings (the common case after a real fix) still shows
up with its installed version -- without that flag it would simply vanish
from the scan output and look identical to "can't tell."

Usage:
  compute_stale_risks.py <accepted-risks.yml> <churn-report-out.md> <scan.json> [<scan.json> ...]

Prints bare CVE/GHSA IDs (one per line, no cve/|osv/ prefix) that are safe
to remove to stdout -- intended to be captured directly as the stale-ids
file consumed by apply_stale_risk_removal.py. Diagnostic/progress lines go
to stderr. A separate markdown note is written to churn-report-out.md for
every entry where the package+version is unchanged but the registered ID
itself no longer appears in the fresh scan -- informational only, never
acted on automatically, so a human notices the relabeling and can decide
whether the registry entry's id needs updating.
"""
import json
import re
import sys

try:
    import yaml
except ImportError:
    sys.exit("PyYAML is required: pip install pyyaml")


def log(msg):
    print(msg, file=sys.stderr)


def package_tokens(raw):
    """Split a registry 'package' field into installed-package-name
    candidates. Handles the shapes actually used in accepted-risks.yml:
    "zlib1g" / "curl, libcurl4" / "ncurses (libncursesw6, libtinfo6)" /
    "http-cache-semantics (npm)" / "python3.11, libpython3.11-*"."""
    raw = raw.strip()
    paren = re.search(r"\(([^)]*)\)", raw)
    if paren and "," in paren.group(1):
        return [t.strip() for t in paren.group(1).split(",") if t.strip()]
    base = re.sub(r"\s*\([^)]*\)\s*$", "", raw).strip()
    return [t.strip() for t in base.split(",") if t.strip()]


def pkg_matches(token, pkg_name):
    if token.endswith("*"):
        return pkg_name.startswith(token[:-1])
    return pkg_name == token


def load_scan(path):
    """Return (pkg_versions: {name: version}, vuln_ids: set(str)) for one
    fresh scan. pkg_versions is built from --list-all-pkgs' full package
    inventory so a package with zero current findings still has a known
    version, unioned with Vulnerabilities' PkgName/InstalledVersion as a
    fallback for older scan JSON that lacks the Packages list."""
    pkg_versions = {}
    vuln_ids = set()
    try:
        with open(path) as f:
            d = json.load(f)
    except Exception as e:
        log(f"DEBUG: {path}: failed to load ({e!r}) -- treating as no data")
        return pkg_versions, vuln_ids
    for res in d.get("Results", []) or []:
        for p in res.get("Packages", []) or []:
            name = p.get("Name")
            pkg_id = p.get("ID") or ""
            # Packages' own "Version" field is SBOM-style (upstream version
            # only, e.g. "1.2.13.dfsg") -- it drops the epoch/revision that
            # both the registry's `version` field and Vulnerabilities'
            # InstalledVersion use (e.g. "1:1.2.13.dfsg-1"). "ID" is
            # "<name>@<full version>" and matches that full form exactly;
            # fall back to reassembling Version+Release+Epoch only if ID is
            # somehow absent or doesn't start with the package name.
            if name and pkg_id.startswith(name + "@"):
                ver = pkg_id[len(name) + 1:]
            elif name:
                ver = p.get("Version")
                if p.get("Release"):
                    ver = f"{ver}-{p['Release']}"
                if p.get("Epoch"):
                    ver = f"{p['Epoch']}:{ver}"
            else:
                ver = None
            if name and ver:
                pkg_versions[name] = ver
        for v in res.get("Vulnerabilities", []) or []:
            vuln_ids.add(v["VulnerabilityID"])
            name, ver = v.get("PkgName"), v.get("InstalledVersion")
            if name and ver:
                pkg_versions.setdefault(name, ver)
    log(f"DEBUG: {path}: {len(pkg_versions)} package(s) known, {len(vuln_ids)} unique vuln ID(s)")
    return pkg_versions, vuln_ids


def main(argv):
    if len(argv) < 3:
        sys.exit(__doc__)
    registry_path, churn_report_path, scan_paths = argv[0], argv[1], argv[2:]

    with open(registry_path) as f:
        entries = (yaml.safe_load(f) or {}).get("entries") or []

    scans = {}  # image name -> (pkg_versions, vuln_ids)
    for path in scan_paths:
        m = re.search(r"scan-(.+)\.json$", path)
        image = m.group(1) if m else path
        scans[image] = load_scan(path)

    stale, churn_notes = [], []
    n_kept = n_skipped = 0

    for e in entries:
        if e.get("kind") != "container":
            continue
        bare_id = e["id"].split("/", 1)[1]
        tokens = package_tokens(e["package"])
        target_version = e["version"]
        applicable = [img for img in (e.get("images") or []) if img in scans]

        if not applicable:
            n_skipped += 1
            log(f"SKIP   {bare_id} ({e['package']!r}): none of its images were scanned this run")
            continue

        still_present = False
        id_seen = False
        for img in applicable:
            pkg_versions, vuln_ids = scans[img]
            for tok in tokens:
                for name, ver in pkg_versions.items():
                    if pkg_matches(tok, name) and ver == target_version:
                        still_present = True
                        break
                if still_present:
                    break
            if bare_id in vuln_ids:
                id_seen = True

        if still_present:
            n_kept += 1
            if not id_seen:
                churn_notes.append(
                    f"- `{bare_id}` ({e['package']}, version `{target_version}`): package is "
                    f"still installed at the exact suppressed version in {', '.join(applicable)}, "
                    f"but this exact ID no longer appears in the fresh scan. The vulnerability "
                    f"database likely relabeled it under a different ID for the same unfixed "
                    f"package -- **not removed automatically**; if a new ID for this package shows "
                    f"up as an unsuppressed finding, that's this same issue wearing a new name."
                )
                log(f"CHURN  {bare_id}: version unchanged, ID not seen this scan -- kept, flagged")
            else:
                log(f"KEEP   {bare_id}: still present, unchanged")
        else:
            stale.append(bare_id)
            log(f"STALE  {bare_id} ({e['package']}, was `{target_version}`): not installed at that "
                f"version in any scanned image it applies to ({', '.join(applicable)}) -- real fix")

    log(f"\nSummary: {len(stale)} stale (version changed), {n_kept} kept, "
        f"{len(churn_notes)} of those flagged as ID churn, {n_skipped} unverifiable this run")

    with open(churn_report_path, "w") as f:
        if churn_notes:
            f.write("## Suppressed CVEs whose ID changed with no real fix (not removed)\n\n")
            f.write(
                "Package + installed version still match the registry entry exactly, but the "
                "registered CVE ID didn't appear in today's fresh scan -- the vulnerability "
                "database relabeled it, not a real fix. Left in place on purpose; if an "
                "unsuppressed finding for the same package shows up and blocks a release, it's "
                "almost certainly this.\n\n"
            )
            f.write("\n".join(churn_notes) + "\n")

    print("\n".join(stale))


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
