#!/usr/bin/env python3
"""Validate security/accepted-risks.yml against the shared datawatch /
datawatch-app security-acceptance schema (dmz006/datawatch#197).

Checks (fail the build / exit 1 on any violation):
  - required fields present: id, kind, package, version, severity, images
    (optional), impact.traced (bool), impact.reachable ("yes"|"no"|
    "unknown"), impact.analysis (non-empty), impact.method (required when
    impact.traced is true), first_added, added, expires, validated_by,
    reason.
  - severity is one of low|medium|moderate|high|critical.
  - id is unique and starts with a recognized kind prefix (cve/, osv/,
    dependabot/, code-scanning/).
  - first_added <= added (dates, ISO YYYY-MM-DD).
  - expires <= added + 90 days when traced, else added + 30 days.
    (Expiry itself -- i.e. whether `expires` is already in the past
    relative to today -- is NOT a lint failure. That is the daily watch's
    job, not this script's: BL398's 2026-10-08 migration intentionally
    imported most entries already past their new, stricter expiry window
    relative to their real historical first_added/added date. Failing the
    build on that would block all unrelated work until the whole backlog
    is cleared, which is not the point of a non-blocking expiry policy.)

ESCALATE lines (printed, does NOT fail the build) when:
  - impact.reachable == "yes" on a high/critical severity entry;
  - added > first_added (entry has been renewed at least once) and today
    is past its first expiry window (first_added + 90d traced / 30d
    untraced at time of first acceptance).

--base <ref>: additionally fails if any entry's first_added differs from
its value in the registry at that git ref (first_added must be immutable
across renewals).

Usage:
  python3 scripts/check_accepted_risks.py [--base <git-ref>] [path]
"""
import datetime
import subprocess
import sys

try:
    import yaml
except ImportError:
    sys.exit("PyYAML is required: pip install pyyaml")

DEFAULT_PATH = "security/accepted-risks.yml"
ALLOWED_SEVERITY = {"low", "medium", "moderate", "high", "critical"}
ALLOWED_REACHABLE = {"yes", "no", "unknown"}
ALLOWED_PREFIXES = ("cve/", "osv/", "dependabot/", "code-scanning/")
REQUIRED_TOP = ["id", "kind", "package", "severity", "impact", "first_added", "added", "expires", "validated_by", "reason"]
VERSION_REQUIRED_KINDS = {"container", "dependency", "bundled-js"}


def parse_date(s, ctx):
    try:
        return datetime.date.fromisoformat(s)
    except (TypeError, ValueError):
        raise ValueError(f"{ctx}: not a valid YYYY-MM-DD date: {s!r}")


def load(path):
    with open(path) as f:
        data = yaml.safe_load(f)
    entries = (data or {}).get("entries") or []
    return entries


def validate(entries):
    errors = []
    escalations = []
    seen_ids = set()
    today = datetime.date.today()

    for e in entries:
        eid = e.get("id", "<missing id>")
        ctx = eid

        for field in REQUIRED_TOP:
            if field not in e or e[field] in (None, ""):
                errors.append(f"{ctx}: missing required field '{field}'")
        if "id" not in e:
            continue

        if eid in seen_ids:
            errors.append(f"{ctx}: duplicate id")
        seen_ids.add(eid)

        if not eid.startswith(ALLOWED_PREFIXES):
            errors.append(f"{ctx}: id must start with one of {ALLOWED_PREFIXES}")

        kind = e.get("kind")
        if kind in VERSION_REQUIRED_KINDS and not e.get("version"):
            errors.append(f"{ctx}: kind={kind} requires a non-empty 'version'")

        sev = e.get("severity")
        if sev not in ALLOWED_SEVERITY:
            errors.append(f"{ctx}: severity {sev!r} not in {sorted(ALLOWED_SEVERITY)}")

        impact = e.get("impact") or {}
        traced = impact.get("traced")
        if not isinstance(traced, bool):
            errors.append(f"{ctx}: impact.traced must be true/false")
        if traced and not impact.get("method"):
            errors.append(f"{ctx}: impact.method is required when impact.traced is true")
        if not impact.get("analysis"):
            errors.append(f"{ctx}: impact.analysis is required")
        reachable = impact.get("reachable")
        if reachable not in ALLOWED_REACHABLE:
            errors.append(f"{ctx}: impact.reachable {reachable!r} must be one of {sorted(ALLOWED_REACHABLE)} (quoted in YAML)")

        try:
            first_added = parse_date(e.get("first_added"), f"{ctx}.first_added")
            added = parse_date(e.get("added"), f"{ctx}.added")
            expires = parse_date(e.get("expires"), f"{ctx}.expires")
        except ValueError as ve:
            errors.append(str(ve))
            continue

        if first_added > added:
            errors.append(f"{ctx}: first_added ({first_added}) is after added ({added})")

        max_window = 90 if traced else 30
        if (expires - added).days > max_window:
            errors.append(f"{ctx}: expires is {(expires - added).days}d after added, exceeds the {max_window}d cap ({'traced' if traced else 'untraced'})")

        # ESCALATE: reachable finding on a high/critical severity.
        if reachable == "yes" and sev in ("high", "critical"):
            escalations.append(f"ESCALATE {eid}: impact.reachable=yes on {sev} severity -- needs operator review, not self-service.")

        # ESCALATE: renewed past its first expiry window.
        if added > first_added:
            first_expiry = first_added + datetime.timedelta(days=max_window)
            if today > first_expiry:
                escalations.append(f"ESCALATE {eid}: renewed ({added}) past its first expiry window ({first_expiry}) -- needs operator review of the renewal, not self-service.")

    return errors, escalations


def check_first_added_immutable(path, base_ref):
    try:
        old_raw = subprocess.run(["git", "show", f"{base_ref}:{path}"], capture_output=True, text=True, check=True).stdout
    except subprocess.CalledProcessError:
        return []  # file didn't exist at base_ref -- nothing to compare
    old = {e["id"]: e.get("first_added") for e in (yaml.safe_load(old_raw) or {}).get("entries") or [] if "id" in e}
    new_entries = load(path)
    errors = []
    for e in new_entries:
        eid = e.get("id")
        if eid in old and old[eid] != e.get("first_added"):
            errors.append(f"{eid}: first_added changed from {old[eid]!r} to {e.get('first_added')!r} since {base_ref} -- first_added is immutable")
    return errors


def main(argv):
    base_ref = None
    args = list(argv)
    if "--base" in args:
        i = args.index("--base")
        base_ref = args[i + 1]
        del args[i:i + 2]
    path = args[0] if args else DEFAULT_PATH

    entries = load(path)
    errors, escalations = validate(entries)

    if base_ref:
        errors.extend(check_first_added_immutable(path, base_ref))

    for line in escalations:
        print(line)

    if errors:
        print(f"\n{len(errors)} error(s) in {path}:", file=sys.stderr)
        for err in errors:
            print(f"  - {err}", file=sys.stderr)
        return 1

    print(f"OK: {len(entries)} entries in {path} pass schema validation"
          f"{f' ({len(escalations)} escalation(s) above)' if escalations else ''}.")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
