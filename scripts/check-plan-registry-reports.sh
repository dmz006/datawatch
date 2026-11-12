#!/usr/bin/env bash
# scripts/check-plan-registry-reports.sh — verifies that the plan-registry
# reconciliation reports are internally consistent, so the "118 files
# covered" claim is checked, not just stated.
#
# It reads (never writes):
#   plan-registry-reconciliation-list.md
#   plan-registry-reconciliation-batch-01.md .. batch-06.md
#   plan-registry-reconciliation-summary.md
#
# and fails (non-zero exit, one clear message per problem) when any of
# these is false:
#   1. the list states a count and has exactly that many entries;
#   2. every listed file appears exactly once across the six batch
#      reports, and no unlisted file appears;
#   3. every batch entry carries a class, one letter from a to d;
#   4. every class b entry carries a proposed registry row;
#   5. every class c entry carries a reason;
#   6. the class counts in the summary equal the counts found in the
#      batch reports, and their total equals the list count.
#
# Usage: check-plan-registry-reports.sh [reports-dir]
#   reports-dir defaults to docs/plans/reports of this repository; a test
#   can point it at a directory of fake reports instead.
#
# Read-only: it does not edit any file and never touches docs/plans/README.md.

set -euo pipefail

# Never reach a tmux server from this script (AGENT.md: verification
# scripts run with TMUX and TMUX_TMPDIR removed).
unset TMUX TMUX_TMPDIR

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPORTS_DIR="${1:-$ROOT/docs/plans/reports}"

python3 - "$REPORTS_DIR" <<'PY'
import os
import re
import sys

base = sys.argv[1]
problems = []

LIST = "plan-registry-reconciliation-list.md"
SUMMARY = "plan-registry-reconciliation-summary.md"
BATCHES = [f"plan-registry-reconciliation-batch-0{i}.md" for i in range(1, 7)]

def read(name):
    path = os.path.join(base, name)
    if not os.path.isfile(path):
        problems.append(f"{name}: file not found in {base}")
        return None
    with open(path, encoding="utf-8") as fh:
        return fh.read()

def table_rows(text):
    """Data rows of the first markdown table: lists of stripped cells."""
    rows = []
    for line in text.splitlines():
        if not line.lstrip().startswith("|"):
            continue
        cells = [c.strip() for c in re.split(r"(?<!\\)\|", line.strip().strip("|"))]
        if not cells or cells[0] == "file":
            continue  # header
        if all(c == "" or re.fullmatch(r":?-{2,}:?", c) for c in cells):
            continue  # separator
        rows.append(cells)
    return rows

# --- 1. the list states a count and has exactly that many entries -------
list_text = read(LIST)
declared = None
listed = []
if list_text is not None:
    m = re.search(r"^count:\s*(\d+)\s*$", list_text, re.M)
    if not m:
        problems.append(f"{LIST}: no 'count: <n>' line found")
    else:
        declared = int(m.group(1))
    listed = [ln[2:].strip() for ln in list_text.splitlines() if ln.startswith("- ")]
    if declared is not None and len(listed) != declared:
        problems.append(
            f"{LIST}: states count {declared} but has {len(listed)} entries"
        )
    dupes = sorted({f for f in listed if listed.count(f) > 1})
    if dupes:
        problems.append(f"{LIST}: entries listed more than once: {', '.join(dupes)}")

# --- 2./3./4./5. the six batch reports -----------------------------------
batch_files = {}  # file name -> how many batch rows carry it
class_counts = {"a": 0, "b": 0, "c": 0, "d": 0}
for name in BATCHES:
    text = read(name)
    if text is None:
        continue
    rows = table_rows(text)
    for cells in rows:
        if len(cells) < 4:
            problems.append(f"{name}: row with {len(cells)} cells, expected 4: {cells}")
            continue
        fname, cls, evidence, proposed = cells[0], cells[1], cells[2], cells[3]
        if not fname:
            problems.append(f"{name}: row with an empty file column: {cells}")
            continue
        batch_files[fname] = batch_files.get(fname, 0) + 1
        if cls not in ("a", "b", "c", "d"):
            problems.append(
                f"{name}: entry '{fname}' has no class a-d (got {cls!r})"
            )
            continue
        class_counts[cls] += 1
        if cls == "b" and not proposed:
            problems.append(f"{name}: class b entry '{fname}' has no proposed row")
        if cls == "c" and not evidence:
            problems.append(f"{name}: class c entry '{fname}' has no reason")

if list_text is not None:
    listed_set = set(listed)
    for fname in sorted(batch_files):
        if fname not in listed_set:
            problems.append(f"batch reports: unlisted file '{fname}' appears")
        elif batch_files[fname] != 1:
            problems.append(
                f"batch reports: listed file '{fname}' appears {batch_files[fname]} times"
            )
    for fname in sorted(listed_set - set(batch_files)):
        problems.append(f"batch reports: listed file '{fname}' appears in no batch report")

# --- 6. the summary counts match the batch reports ------------------------
summary_text = read(SUMMARY)
if summary_text is not None:
    m = re.search(r"^##\s*Counts per class\s*$(.*?)(?=^##\s|\Z)",
                  summary_text, re.M | re.S)
    block = m.group(1) if m else summary_text
    stated = {cls: int(n) for cls, n in re.findall(r"class\s+([abcd])\s*:\s*(\d+)", block)}
    for cls in "abcd":
        got, want = class_counts[cls], stated.get(cls)
        if want is None:
            problems.append(f"{SUMMARY}: no stated count for class {cls}")
        elif want != got:
            problems.append(
                f"{SUMMARY}: class {cls} count is {want} but the batch reports have {got}"
            )
    tm = re.search(r"total\s*:\s*(\d+)", block)
    if not tm:
        problems.append(f"{SUMMARY}: no stated total")
    else:
        total = int(tm.group(1))
        if total != sum(class_counts.values()):
            problems.append(
                f"{SUMMARY}: total {total} does not equal the batch class-count sum {sum(class_counts.values())}"
            )
        if declared is not None and total != declared:
            problems.append(
                f"{SUMMARY}: total {total} does not equal the list count {declared}"
            )

if problems:
    for p in problems:
        print(f"FAIL: {p}", file=sys.stderr)
    print(f"FAIL: {len(problems)} problem(s) in the plan-registry reconciliation reports",
          file=sys.stderr)
    sys.exit(1)

print(f"OK: list count {declared} matches {sum(class_counts.values())} batch entries "
      f"(a {class_counts['a']}, b {class_counts['b']}, c {class_counts['c']}, d {class_counts['d']}); "
      "every entry classified, class b rows and class c reasons present, summary consistent.")
PY
