#!/usr/bin/env python3
"""Registry hygiene for docs/plans/README.md (standard library only).

Operates on the registry (docs/plans/README.md) of the repo whose root is
the first positional argument, and applies the project-tracking rules from
AGENT.md ("Project Tracking (docs/plans/README.md)"):

  1. remove duplicate sections — a heading that appears more than once is
     kept once; the body of the later copies (table lines and list bullets)
     is folded into the first section, then the duplicate heading goes;
  2. merge completed tables into one — tables whose rows are all closed
     collapse into the first such table ("keep only ONE completed bugs
     table"); the later tables are removed;
  3. dedupe duplicate rows — the same row (id + item text) appearing in
     more than one section (backlog and planned, per the AGENT.md rule
     "do not leave duplicates in both sections") is kept at its first
     occurrence;
  4. trim closed rows to a one-line summary — a closed status cell longer
     than 120 chars is truncated at a word boundary with an ellipsis;
  5. correct stale draft / not-started rows — when a status file (the
     Automaton list export, supplied as a JSON file) says a referenced PRD
     is past a live state, a row still claiming draft / not started /
     planning / waiting / pending / queued is rewritten with that state.
     The file is read as-is: no daemon, no gh.

Modes:
  default      apply the changes
  --dry-run    report counts only, change nothing

Output is counts only — never a list of files.

Status file format (any of):
  [{"id": "1234abcd", "status": "completed"}, ...]
  {"1234abcd": "completed", ...}
  {"prds": [...]}    or    {"automata": [...]}
List items may also use "prd_id" instead of "id".

Usage:
  plans_registry_hygiene.py REPO_ROOT [--dry-run] [--status-file PATH]
                            [--archive-month YYYY-MM]

Closed-row archive pass (runs last): closed rows of stage tables move to
docs/plans/roadmap-archive-YYYY-MM.md (UTC month of the run, or
--archive-month), except rows of the current stage (the stage section
holding the lowest-numbered open row).  Rows keep their number and text;
the number cell gets an anchor <a id="row-N"></a> so links still resolve.
Rows outside stage tables (Rules, Design docs, ...) are never archived.
"""

import argparse
import datetime
import json
import re
import sys
from pathlib import Path

TRIM_LIMIT = 120
STALE_PREFIX = 80

TERMINAL_CELL_RE = re.compile(
    r"(?i)^\s*[*\s]*(done|shipped|shipped in|completed|closed|merged|fixed|landed|delivered)\b"
)
STALE_RE = re.compile(r"(?i)\b(draft|not started|planning|waiting|pending|queued)\b")
ID_RE = re.compile(r"\b[0-9a-f]{8}\b")

LIVE_STATES = {
    "draft", "planned", "planning", "pending", "queued", "running",
    "in_progress", "in progress", "decomposed", "approved", "blocked",
    "needs_review", "held", "waiting", "open",
}
TERMINAL_STATES = {
    "done", "shipped", "completed", "completed_and_verified", "closed",
    "merged", "fixed", "failed", "cancelled", "canceled", "superseded",
    "landed", "archived",
}

COUNT_NAMES = (
    "completed tables merged",
    "duplicate sections removed",
    "duplicate rows removed",
    "closed rows trimmed",
    "stale rows corrected",
    "closed rows archived",
    "month lines kept or updated",
    "links rewritten",
)


def flat(cell):
    """Cell text flattened for keying: link targets to their text, markers off."""
    cell = re.sub(r"\[([^\]]*)\]\([^)]*\)", r"\1", cell)
    return re.sub(r"[*_`]", "", cell).lower().strip()


def cells_of(line):
    s = line.strip()
    return [c.strip() for c in s[1:-1].split("|")]


def is_separator(line):
    s = line.strip()
    if not (s.startswith("|") and s.endswith("|")):
        return False
    parts = [p.strip() for p in s.strip("|").split("|")]
    return bool(parts) and all(re.fullmatch(r":?-{1,}:?", p) for p in parts)


def is_table_row(line):
    s = line.strip()
    return s.startswith("|") and s.endswith("|") and len(s) >= 2


def heading_of(line):
    m = re.match(r"^(#{1,6})\s+(.*?)\s*#*\s*$", line)
    if not m:
        return None
    return (len(m.group(1)), m.group(2).strip())


def truncate(text, limit):
    text = re.sub(r"\s+", " ", text).strip()
    if len(text) <= limit:
        return text
    cut = text.rfind(" ", int(limit * 0.5), limit)
    return (text[:cut].rstrip() if cut != -1 else text[:limit]) + "\u2026"


def collapse_blank_runs(lines):
    out, blank = [], 0
    for line in lines:
        if line.strip() == "":
            blank += 1
            if blank > 1:
                continue
        else:
            blank = 0
        out.append(line)
    return out


class Table:
    def __init__(self, start, end, header, sep, rows):
        self.start = start        # line index of the header line
        self.end = end            # line index of the last body row
        self.header = header      # list of header cell texts
        self.sep = sep            # separator line
        self.rows = rows          # body row lines (original text)

    def status_idx(self):
        for i, c in enumerate(self.header):
            if "status" in flat(c):
                return i
        if len(self.header) > 2:
            return 2
        return len(self.header) - 1

    def status_of(self, rowline):
        cells = cells_of(rowline)
        if not cells:
            return ""
        idx = self.status_idx()
        return cells[idx] if idx < len(cells) else cells[-1]

    def is_closed_row(self, rowline):
        cell = self.status_of(rowline).strip()
        if cell[:8].lower().startswith(("not ", "no ")):
            return False
        return bool(TERMINAL_CELL_RE.search(cell))

    def all_closed(self):
        return bool(self.rows) and all(self.is_closed_row(r) for r in self.rows)

    def replace_status(self, rowline, newcell):
        cells = cells_of(rowline)
        idx = self.status_idx()
        if idx >= len(cells):
            cells.append(newcell)
        else:
            cells[idx] = newcell
        return "| " + " | ".join(cells) + " |"

    def write_into(self, lines):
        """Rewrite this table (header line, separator, rows) starting at self.start."""
        lines[self.start:self.end + 1] = [lines[self.start], self.sep] + self.rows


def find_tables(lines):
    tables = []
    i, n = 0, len(lines)
    while i < n:
        if is_table_row(lines[i]) and i + 1 < n and is_separator(lines[i + 1]):
            rows = []
            j = i + 2
            while j < n and is_table_row(lines[j]) and not is_separator(lines[j]):
                rows.append(lines[j])
                j += 1
            tables.append(Table(i, i + 1 + len(rows), cells_of(lines[i]),
                                lines[i + 1], rows))
            i = j
        else:
            i += 1
    return tables


def row_key(line):
    """Exact-duplicate key: id + item + status, all flattened and lowercased.
    Rows that merely share id + item across stages (different status cells)
    are not duplicates."""
    cells = [flat(c) for c in cells_of(line)]
    if not cells:
        return ""
    return "\u0001".join(cells[:3])


def load_statuses(path):
    try:
        data = json.loads(Path(path).read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        sys.stderr.write("hygiene: cannot read status file: %s\n" % exc)
        raise SystemExit(2)
    items = None
    if isinstance(data, list):
        items = data
    elif isinstance(data, dict):
        for key in ("prds", "automata", "items"):
            if isinstance(data.get(key), list):
                items = data[key]
                break
        if items is None and not any(isinstance(v, list) for v in data.values()):
            return {k.strip().lower(): v.strip() for k, v in data.items()
                    if isinstance(v, str)}
    out = {}
    for it in items or []:
        if not isinstance(it, dict):
            continue
        ident = it.get("id") or it.get("prd_id") or it.get("PRD")
        status = it.get("status") or it.get("state")
        if ident and status:
            out[str(ident).strip().lower()] = str(status).strip()
    return out


def collapse_duplicate_sections(lines, counts):
    groups = {}
    for i, line in enumerate(lines):
        h = heading_of(line)
        if h:
            groups.setdefault(h[1].lower(), []).append((i, h[0]))
    dups = []
    for name, occ in groups.items():
        for idx, level in occ[1:]:
            dups.append((idx, level))
    for idx, level in sorted(dups, reverse=True):
        if heading_of(lines[idx]) is None:
            continue
        first_idx = groups[heading_of(lines[idx])[1].lower()][0][0]
        # capture this section's body: table lines and list bullets, up to
        # the next heading of this level or higher (blank lines skipped)
        moved, last_body, j = [], None, idx + 1
        while j < len(lines):
            h = heading_of(lines[j])
            if h and h[0] <= level:
                break
            s = lines[j].strip()
            if s == "":
                j += 1
                continue
            if is_table_row(lines[j]) or re.match(r"^[-*+]\s+", s):
                moved.append(lines[j].rstrip())
                last_body = j
                j += 1
            else:
                break
        # drop the duplicate heading and its body BEFORE any insertion
        # shifts the indices (first_idx < idx, so it stays valid)
        end = last_body if last_body is not None else idx
        while end + 1 < len(lines) and lines[end + 1].strip() == "" \
                and last_body is None:
            end += 1
        del lines[idx:end + 1]
        if moved:
            lines[first_idx + 1:first_idx + 1] = [""] + moved
        counts["duplicate sections removed"] += 1
    lines = remove_empty_sections(lines, counts)
    return collapse_blank_runs(lines)

def remove_empty_sections(lines, counts):
    """Drop headings whose section holds no content (e.g. a table that was
    merged elsewhere).  The document title (a lone `#` heading at the top)
    is kept."""
    headings = []
    for i, line in enumerate(lines):
        h = heading_of(line)
        if h and h[0] >= 2:
            headings.append((i, h[0]))
    drop = set()
    for pos, (i, level) in enumerate(headings):
        end = headings[pos + 1][0] if pos + 1 < len(headings) else len(lines)
        if any(lines[j].strip() for j in range(i + 1, end)):
            continue
        k = i
        while k + 1 < min(end, len(lines)) and lines[k + 1].strip() == "":
            k += 1
        drop.update(range(i, k + 1))
        counts["duplicate sections removed"] += 1
    kept = [line for i, line in enumerate(lines) if i not in drop]
    return collapse_blank_runs(kept)


def merge_closed_tables(lines, counts):
    tables = find_tables(lines)
    closed_tables = [t for t in tables if t.all_closed()]
    if len(closed_tables) < 2:
        return lines
    first = closed_tables[0]
    seen = {row_key(r) for r in first.rows}
    new_rows = []
    for t in closed_tables[1:]:
        for r in t.rows:
            key = row_key(r)
            if key in seen:
                counts["duplicate rows removed"] += 1  # folded duplicate dropped
                continue
            seen.add(key)
            new_rows.append(r)
    # drop the later closed tables first (reverse file order keeps the
    # survivor's line indices valid), then expand the survivor
    for t in sorted(closed_tables[1:], key=lambda t: -t.start):
        del lines[t.start:t.end + 1]
    first.rows = first.rows + new_rows
    first.write_into(lines)
    counts["completed tables merged"] += len(closed_tables) - 1
    lines = remove_empty_sections(lines, counts)
    return collapse_blank_runs(lines)


def dedupe_rows(lines, counts):
    seen_rows = set()  # spans the whole file: backlog and planned must not both keep the same entry
    edits = []
    for t in find_tables(lines):
        kept = []
        for r in t.rows:
            key = row_key(r)
            if key in seen_rows:
                counts["duplicate rows removed"] += 1
                continue
            seen_rows.add(key)
            kept.append(r)
        if len(kept) != len(t.rows):
            edits.append((t, kept))
    # apply writes in reverse file order so earlier tables' indices stay valid
    for t, kept in sorted(edits, key=lambda e: -e[0].start):
        t.rows = kept
        t.write_into(lines)
    return lines


def trim_closed_rows(lines, counts):
    for t in find_tables(lines):
        kept = []
        for r in t.rows:
            cell = t.status_of(r)
            if t.is_closed_row(r) and len(cell) > TRIM_LIMIT:
                counts["closed rows trimmed"] += 1
                kept.append(t.replace_status(r, truncate(cell, TRIM_LIMIT)))
            else:
                kept.append(r)
        t.rows = kept
        t.write_into(lines)
    return lines


def correct_stale_rows(lines, counts, statuses):
    if not statuses:
        return lines
    for t in find_tables(lines):
        idx = t.status_idx()
        kept = []
        for r in t.rows:
            cells = cells_of(r)
            replaced = None
            if idx < len(cells):
                cell = cells[idx]
                if STALE_RE.search(cell):
                    scope = cell + " " + (cells[1] if len(cells) > 1 else "")
                    for ident in ID_RE.findall(scope):
                        status = statuses.get(ident.lower())
                        norm = " ".join((status or "").lower().split())
                        if not status or norm in LIVE_STATES:
                            continue
                        if norm in TERMINAL_STATES:
                            old = (truncate(cell, STALE_PREFIX) if len(cell) > STALE_PREFIX
                                   else cell.strip())
                            replaced = t.replace_status(
                                r,
                                "corrected (list: PRD `%s` %s): %s"
                                % (ident, status.strip(), old),
                            )
                            counts["stale rows corrected"] += 1
                            break
            kept.append(replaced if replaced else r)
        t.rows = kept
        t.write_into(lines)
    return lines

STAGE_HEADING_RE = re.compile(r"(?i)^stage\b")
ROW_NUM_RE = re.compile(r"^\s*(?:<a id=\"row-(\d+)\"></a>)?\**\s*(\d+)\b")
ARCHIVE_NOTE = ("Closed rows moved out of the live registry (README.md). "
                "Row numbers are unchanged; each number carries a row-N anchor.")


def row_number(rowline):
    cells = cells_of(rowline)
    m = ROW_NUM_RE.match(cells[0]) if cells else None
    if not m:
        return None
    return int(m.group(1) or m.group(2))


def stage_tables(lines):
    """[(heading text, Table)] for tables under a Stage heading."""
    out = []
    for t in find_tables(lines):
        head = None
        for i in range(t.start - 1, -1, -1):
            h = heading_of(lines[i])
            if h:
                head = h[1]
                break
        if head is not None and STAGE_HEADING_RE.match(head):
            out.append((head, t))
    return out


def anchored(rowline, num):
    cells = cells_of(rowline)
    if not cells or '<a id="row-%d"></a>' % num in cells[0]:
        return rowline
    cells[0] = '<a id="row-%d"></a>%s' % (num, cells[0])
    return "| " + " | ".join(cells) + " |"


def archive_closed_rows(lines, counts, archive_path, month, dry_run):
    stages = stage_tables(lines)
    open_nums = []
    for head, t in stages:
        for r in t.rows:
            n = row_number(r)
            if n is not None and not t.is_closed_row(r):
                open_nums.append((n, head))
    current = min(open_nums)[1] if open_nums else None

    moves = {}   # heading -> (table, [(num, row)])
    drops = []   # (table, kept_rows)
    for head, t in stages:
        if head == current:
            continue
        kept, moved = [], []
        for r in t.rows:
            n = row_number(r)
            if n is not None and t.is_closed_row(r):
                moved.append((n, r))
            else:
                kept.append(r)
        if moved:
            entry = moves.setdefault(head, (t, []))
            entry[1].extend(moved)
            drops.append((t, kept))
    if not moves:
        return lines

    alines = (archive_path.read_text(encoding="utf-8").split("\n")
              if archive_path.is_file() else
              ["# Roadmap archive %s" % month, "", ARCHIVE_NOTE])
    existing = set()
    for _, at in stage_tables(alines) + [(None, x) for x in find_tables(alines)]:
        for r in at.rows:
            n = row_number(r)
            if n is not None:
                existing.add(n)

    archived = 0
    for head, (src, moved) in moves.items():
        fresh = []
        for n, r in moved:
            if n in existing:
                continue
            existing.add(n)
            fresh.append(anchored(r, n))
        if not fresh:
            continue
        archived += len(fresh)
        target = next((at for h, at in stage_tables(alines) if h == head), None)
        if target is not None:
            target.rows = target.rows + fresh
            target.write_into(alines)
        else:
            while alines and alines[-1].strip() == "":
                alines.pop()
            alines += ["", "## " + head, "", "| " + " | ".join(src.header) + " |",
                       src.sep] + fresh
    counts["closed rows archived"] += archived

    if dry_run:
        return lines
    for t, kept in sorted(drops, key=lambda d: -d[0].start):
        t.rows = kept
        t.write_into(lines)
    archive_path.write_text("\n".join(alines).rstrip("\n") + "\n", encoding="utf-8")
    return lines


MONTH_LINE_RE = re.compile(
    r"^- \[Closed rows archived for (\d{4}-\d{2})\]\(roadmap-archive-(\d{4}-\d{2})\.md\)")
ARCHIVE_FILE_RE = re.compile(r"^roadmap-archive-(\d{4}-\d{2})\.md$")
ROW_LINK_RE = re.compile(r"(?<![\w./-])((?:[\w./-]*/)?)README\.md#row-(\d+)\b")
LINK_DIRS = ("docs", "scripts")


def archive_rows(plans_dir):
    """{month: [row numbers]} for every roadmap-archive-YYYY-MM.md file."""
    out = {}
    for f in sorted(plans_dir.glob("roadmap-archive-*.md")):
        m = ARCHIVE_FILE_RE.match(f.name)
        if not m:
            continue
        al = f.read_text(encoding="utf-8").split("\n")
        nums = sorted({row_number(r) for t in find_tables(al) for r in t.rows
                       if row_number(r) is not None})
        out[m.group(1)] = nums
    return out


def keep_month_lines(lines, counts, months):
    """One line per archived month under Active order; updated in place."""
    lines = list(lines)
    wanted = {}
    for month, nums in months.items():
        if nums:
            wanted[month] = "- [Closed rows archived for %s](roadmap-archive-%s.md), rows %d to %d" % (
                month, month, nums[0], nums[-1])
    if not wanted:
        return lines
    seen = set()
    out = []
    last_idx = None
    for ln in lines:
        m = MONTH_LINE_RE.match(ln)
        if m and m.group(2) in wanted:
            if m.group(2) in seen:
                continue            # never keep a duplicate
            seen.add(m.group(2))
            ln = wanted[m.group(2)]
            last_idx = len(out)
        out.append(ln)
    lines = out
    missing = [mo for mo in sorted(wanted) if mo not in seen]
    if missing:
        at = None
        if last_idx is not None:
            at = last_idx + 1
        else:
            for i, ln in enumerate(lines):
                h = heading_of(ln)
                if h and h[1].strip().lower() == "active order":
                    at = i + 1
                    while at < len(lines) and lines[at].strip() == "":
                        at += 1
                    lines[at:at] = [wanted[mo] for mo in missing] + [""]
                    missing = []
                    break
            if missing:
                return lines        # no Active order heading: nothing to anchor to
        if missing:
            lines[at:at] = [wanted[mo] for mo in missing]
    counts["month lines kept or updated"] += len(wanted)
    return lines


def rewrite_row_links(root, counts, months, dry_run):
    """Point links at archived live-registry rows to the archive anchor."""
    row_month = {}
    for month in sorted(months):
        for n in months[month]:
            row_month[n] = month
    if not row_month:
        return
    root = Path(root)
    files = [p for d in LINK_DIRS for p in (root / d).rglob("*")
             if p.is_file() and p.suffix in (".md", ".sh", ".py")]
    files += [p for p in root.glob("*.md")]
    for f in files:
        if ARCHIVE_FILE_RE.match(f.name):
            continue
        try:
            text = f.read_text(encoding="utf-8")
        except (UnicodeDecodeError, OSError):
            continue
        hits = [0]

        def sub(m):
            n = int(m.group(2))
            mo = row_month.get(n)
            if mo is None:
                return m.group(0)
            hits[0] += 1
            return "%sroadmap-archive-%s.md#row-%d" % (m.group(1), mo, n)

        new = ROW_LINK_RE.sub(sub, text)
        if hits[0]:
            counts["links rewritten"] += hits[0]
            if not dry_run and new != text:
                f.write_text(new, encoding="utf-8")


def main(argv=None):
    ap = argparse.ArgumentParser(
        description="registry hygiene for docs/plans/README.md (counts only output)")
    ap.add_argument("repo_root")
    ap.add_argument("--dry-run", action="store_true",
                    help="report counts only, change nothing")
    ap.add_argument("--status-file", default=None,
                    help="Automaton list export (JSON) used to correct stale rows")
    ap.add_argument("--archive-month", default=None, metavar="YYYY-MM",
                    help="archive month override (default: UTC month of the run)")
    args = ap.parse_args(argv)
    if args.archive_month and not re.fullmatch(r"\d{4}-(0[1-9]|1[0-2])", args.archive_month):
        ap.error("--archive-month must be YYYY-MM")

    readme = Path(args.repo_root) / "docs" / "plans" / "README.md"
    counts = {name: 0 for name in COUNT_NAMES}
    if not readme.is_file():
        for name in COUNT_NAMES:
            print("hygiene %s: 0" % name)
        return 0

    statuses = load_statuses(args.status_file) if args.status_file else {}
    original = readme.read_text(encoding="utf-8")
    lines = original.split("\n")

    lines = collapse_duplicate_sections(lines, counts)
    lines = merge_closed_tables(lines, counts)
    lines = dedupe_rows(lines, counts)
    lines = trim_closed_rows(lines, counts)
    lines = correct_stale_rows(lines, counts, statuses)

    new_text = "\n".join(lines)
    if not args.dry_run and new_text != original:
        readme.write_text(new_text, encoding="utf-8")

    # archive pass: reads the file as it now stands (after the passes above)
    month = args.archive_month or datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m")
    archive_path = readme.parent / ("roadmap-archive-%s.md" % month)
    lines = new_text.split("\n")
    lines = archive_closed_rows(lines, counts, archive_path, month, args.dry_run)
    months = archive_rows(readme.parent)
    lines = keep_month_lines(lines, counts, months)
    final = "\n".join(lines)
    if not args.dry_run and final != new_text:
        readme.write_text(final, encoding="utf-8")
    rewrite_row_links(args.repo_root, counts, months, args.dry_run)

    for name in COUNT_NAMES:
        print("hygiene %s: %d" % (name, counts[name]))
    return 0


if __name__ == "__main__":
    sys.exit(main())
