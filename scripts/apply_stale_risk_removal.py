#!/usr/bin/env python3
"""Remove genuinely-fixed entries from security/accepted-risks.yml.

Used by the `recheck-ignored-cves` job in .github/workflows/image-refresh.yaml
after it rebuilds every real shipped image fresh (no cache) and scans each
one WITHOUT .trivyignore applied. Any accepted CVE absent from every one of
those fresh, unsuppressed scans has a real upstream fix available now.

Supersedes the old apply-stale-cve-removal.py, which edited .trivyignore
directly -- .trivyignore is now GENERATED from this registry
(scripts/gen_trivyignore.py), so removal has to happen here and the caller
must regenerate afterward.

The registry is edited as plain text (entry-block removal), not via a full
YAML parse-and-rewrite, so untouched entries' exact formatting/comments are
never disturbed by a round-trip.

Usage: apply_stale_risk_removal.py <accepted-risks.yml path> <stale-ids-file>
Prints a markdown summary (for the PR body) to stdout.
"""
import re
import sys

ENTRY_START = re.compile(r"^\s*-\s*id:\s*(cve|osv|dependabot|code-scanning)/(\S+)\s*$")


def parse_entries(lines):
    """Return (header_lines, [(cve_id, entry_lines), ...])."""
    header_end = None
    for i, line in enumerate(lines):
        if ENTRY_START.match(line):
            header_end = i
            break
    if header_end is None:
        return lines, []
    header = lines[:header_end]
    entries = []
    i = header_end
    n = len(lines)
    while i < n:
        m = ENTRY_START.match(lines[i])
        if not m:
            i += 1
            continue
        cid = m.group(2)
        start = i
        i += 1
        while i < n and not ENTRY_START.match(lines[i]):
            i += 1
        entries.append((cid, lines[start:i]))
    return header, entries


def main():
    if len(sys.argv) != 3:
        print("usage: apply_stale_risk_removal.py <accepted-risks.yml> <stale-ids-file>", file=sys.stderr)
        sys.exit(2)
    registry_path, stale_path = sys.argv[1], sys.argv[2]

    with open(stale_path) as f:
        stale = {ln.strip() for ln in f if ln.strip()}
    if not stale:
        print("No stale CVE IDs provided — nothing to do.")
        return

    with open(registry_path) as f:
        lines = [ln.rstrip("\n") for ln in f]

    header, entries = parse_entries(lines)
    if not entries:
        print("No entries found in registry — nothing to do.")
        return

    removed = [(cid, block) for cid, block in entries if cid in stale]
    kept = [(cid, block) for cid, block in entries if cid not in stale]

    if not removed:
        print("None of the stale IDs matched a current registry entry — nothing to do.")
        return

    out_lines = list(header)
    for cid, block in kept:
        out_lines.extend(block)
    while out_lines and out_lines[-1].strip() == "":
        out_lines.pop()
    out_lines.append("")

    with open(registry_path, "w") as f:
        f.write("\n".join(out_lines) + "\n")

    print("## Entries removed from security/accepted-risks.yml (fix now available upstream)\n")
    for cid, _ in removed:
        print(f"- `{cid}`")
    print("\nRun `python3 scripts/gen_trivyignore.py` to regenerate `.trivyignore` and "
          "`docs/security-review.md`'s table from the trimmed registry.")


if __name__ == "__main__":
    main()
