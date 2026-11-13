---
docs:
  index: true
  topics: [autonomous, prd, automation, shipped, release, backfill, counts, dashboard]
exec_params:
  - {name: id, required: true, description: "The Automaton (PRD) or Automation id to act on"}
  - {name: release, required: false, description: "Release the work was merged into, e.g. v1.2.0"}
  - {name: commit, required: true, description: "The merge commit on the default branch (required — it is what makes the record shipped)"}
  - {name: merged_at, required: false, description: "Merge time, RFC 3339 (default now)"}
  - {name: source, required: false, description: "Where the record came from: merge_step or backfill (default backfill)"}
  - {name: automation, required: false, description: "Record an Automation instead of a PRD: true or false (default false)"}
  - {name: dry_run, required: false, description: "Backfill: scan and compute the report without writing: true or false (default false)"}
exec_steps:
  - tool: autonomous_shipped_record
    description: Record the shipped state of an Automaton (PRD) or an Automation
    args:
      id: "{{params.id}}"
      release: "{{params.release}}"
      commit: "{{params.commit}}"
      source: "{{params.source}}"
      automation: "{{params.automation}}"
    read_only: false
  - tool: autonomous_shipped_backfill
    description: Run one shipped-state backfill pass over the base repository's default-branch merge history
    args:
      dry_run: "{{params.dry_run}}"
    read_only: false
  - tool: autonomous_shipped_counts
    description: Read the per-status shipped counts (PRD + Automation buckets) under the in-force autonomous.shipped settings
    args: {}
    read_only: true
  - tool: config_set
    description: Adjust one autonomous.shipped.* setting (enabled, show_chip, filter_default, count_separately, backfill_on_start)
    args: {}
    read_only: false
---

# How-to: The shipped status

## 1. When you need it

Every Automaton (PRD) and Automation that has finished carries its stored
status — `completed`, `cancelled`, and so on. The **shipped** state answers a
question the status does not: **has the work actually landed on the default
branch, and in which release?** A PRD can complete before anything merges;
a cancelled Automaton can still have its work merged. Shipped is the answer,
and it is independent:

- **shipped is not a status.** Recording shipped state never touches the
  stored status, and the status never implies shipped. A cancelled Automaton
  whose branch merged stays `cancelled` in the store and counts as
  **shipped** in the shipped view. Both statements are true at once.
- A record **is shipped** when it carries a non-empty **merge commit**
  (`merge_commit`). That field is the whole test.
- Each record keeps four shipped fields with the story of how the work
  landed:

  | Field | Meaning |
  |-------|---------|
  | `merged_in_release` | the release the work landed in, e.g. `v1.2.0` |
  | `merged_at` | when it merged (RFC 3339; unset when the time is unknown) |
  | `merge_commit` | the default-branch merge commit — **non-empty is what makes the record shipped** |
  | `shipped_source` | where the record came from: `merge_step` or `backfill` |

- **idempotent per commit.** Recording the same commit again is a no-op that
  leaves the stored fields, the revision and the updated time alone. A
  different commit overwrites all four fields.

## 2. How shipped shows up

While `autonomous.shipped.enabled` and `count_separately` hold, shipped
records are drawn out of their status bucket and counted as a **shipped**
bucket of their own — across the counts, the list views and every read
surface. A cancelled-and-shipped Automaton is `cancelled` in the store and
`shipped` in the shipped view. Turn `count_separately` off and a
shipped-yet-cancelled record folds back into `cancelled`; the shipped fields
stay on the record and only the display changes.

## 3. Configuration

Five keys, one block — the whole surface is described by them:

```yaml
autonomous:
  shipped:
    enabled: true            # master switch for shipped-state recording and display (default)
    show_chip: true          # render the shipped chip on shipped items (default)
    filter_default: all      # the shipped-state views open on: all (default) | shipped | unshipped
    count_separately: true   # shipped items count in their own bucket (default)
    backfill_on_start: false # run the release backfill at daemon startup (default off)
```

Every value is refused on write if it is not one of the legal ones, and a
blank `filter_default` reads as `all`. Set a key with `PUT /api/config`
(body `{"autonomous.shipped.filter_default": "shipped"}`) or through the MCP
`config_set` tool (key `autonomous.shipped.filter_default`). They are
daemon-wide: there is no per-Automaton override.

You do not need to set any of them: everything ships on except
`backfill_on_start`, which is off so a daemon start never writes records by
itself.

## 4. Recording shipped state — a real example

An Automaton `a1b2c3d4` finished, its `automaton/a1b2c3d4` branch merged to
the default branch as commit `9f8e7d6…`, and the release is `v1.2.0`.

### CLI

```
$ datawatch autonomous shipped record a1b2c3d4 \
    --release v1.2.0 --commit 9f8e7d6c5… --source merge_step \
    --at 2026-11-01T10:30:00Z
```

`--release`, `--commit` are the core of the call; `--commit` is required,
`--at` defaults to now, `--source` defaults to `backfill` (use
`merge_step` when you record the moment the work landed). Add `--automation`
to record an Automation instead of a PRD:

```
$ datawatch automation shipped <automation id> --release v1.2.0 --commit 9f8e7d6c5…
```

### REST

```
POST /api/autonomous/prds/a1b2c3d4/shipped
Content-Type: application/json

{"merged_in_release":"v1.2.0","merged_at":"2026-11-01T10:30:00Z",
 "merge_commit":"9f8e7d6c5…","source":"merge_step"}
```

`source` must be `merge_step` or `backfill`; the commit must be non-empty.
The 200 body is the record as stored. The Automation form is the same body
on `POST /api/automations/<id>/shipped`. A repeat with the same commit
returns 200 and changes nothing.

### MCP

The tool `autonomous_shipped_record` takes `id`, `merge_commit` (required),
`merged_in_release`, `merged_at`, `source` (`merge_step` or `backfill`) and
`automation` — `true` to record an Automation instead of a PRD. It calls the
same server endpoint, so it has the same idempotency, the same validation
and the same audit record.

### Comm channel

```
autonomous shipped-record a1b2c3d4 commit=9f8e7d6c5... release=v1.2.0 source=merge_step
autonomous shipped-record <automation id> commit=9f8e7d6c5... automation=true release=v1.2.0
```

### The merge helper (no operator at the keyboard)

`scripts/merge-branches-bump-version.sh` sources
`scripts/merge-shipped-record.sh`, and after the release bump commit it
records shipped state for every merged `automaton/<id>` branch best-effort
— the recording is `source=merge_step`, a failed record never fails a merge
that succeeded, and when a PRD's Automation has all non-skipped members
shipped the matching Automation is recorded too.

## 5. Reading the shipped state

### Counts

Every surface reads the same pair: per-status buckets for PRDs and for
Automations, computed under the in-force `autonomous.shipped` settings,
with those settings riding along.

```
$ datawatch autonomous shipped counts
```

```
GET /api/autonomous/shipped/counts
→ {
    "prds":        {"completed": 4, "cancelled": 1, "shipped": 3},
    "automations": {"completed": 1, "shipped": 2},
    "settings":    {"enabled": true, "show_chip": true,
                    "filter_default": "all", "count_separately": true}
  }
```

MCP: `autonomous_shipped_counts`. Comm:

```
autonomous shipped-counts
```

### The lists

- **PRDs** — `GET /api/autonomous/prds?shipped=true|false` (an explicit
  value wins; absent it, `autonomous.shipped.filter_default` decides).
  `status=…` may include `shipped` as one of the statuses, which selects the
  shipped bucket on the *status* axis. CLI: `datawatch autonomous list
  --shipped true`. MCP: `autonomous_prd_list` with `shipped: "true"` /
  `"false"` (or `status: "shipped,…"`).
- **Automations** — `GET /api/automations?shipped=true|false` applies the
  same selection to Automation records. CLI:
  `datawatch automation list --shipped true`. MCP: `automation_list` with
  `shipped`.

The rows carry the four shipped fields when they are present; a record
without shipped state simply lacks them.

### The dashboard card (server-served; companion-app parity pending)

The server fills a **system Shipped card** — present in the factory dashboard
layout, re-added if removed, cannot be deleted — with the live per-status
counts and the in-force settings: `GET /api/dashboard/cards/shipped` (or the
`data` field of the `shipped` entry in `GET /api/dashboard/cards` / `PUT /api/dashboard/cards`).
Today the PWA renders no shipped card (no dashboard-card definition, no
chip, no filter), so that endpoint is the surface an operator reads it on;
the companion-app work that closes the PWA/Android/iOS gap is tracked in
the parity issue [`plans/2026-11-01-shipped-status-parity-issue.md`](../plans/2026-11-01-shipped-status-parity-issue.md).

## 6. Backfilling shipped state from merge history

Newer records are recorded as the work lands; older records were not.
**Backfill** is the recovery: one pass over the base repository's default
branch (the `MAIN_BRANCH` env var, or `main` when unset), reading its merge
commits:

- every merge commit naming an `automaton/<id>` branch records shipped state
  (`source=backfill`) on the matching Automaton (where one exists), with the
  release name and merge time recovered from the history (a version number,
  or the release heading);
- an Automation is marked when it has at least one non-skipped member whose
  non-skipped members are **all** shipped — the mark carries the latest of
  the members' merge facts;
- it never touches any status — a cancelled Automaton whose merge commit
  is on the branch is marked shipped while staying `cancelled`, and one
  with no merge stays plain cancelled (it is what `left_cancelled`
  counts);
- a record that already carries shipped state is counted, not
  re-recorded (idempotent);
- one bad record lands in the report's `errors` and the pass carries on.

### Dry run first

```
$ datawatch autonomous shipped backfill --dry-run
```

```
POST /api/autonomous/shipped/backfill?dry_run=true
```

A dry run scans and computes the report without writing a single record.
The report is the same in both modes: `prds_marked` and
`prds_would_mark_cancelled` both count what the pass would mark, so a dry
run and the real run agree on them:

```
{
  "prds_scanned": 42, "prds_marked": 3, "prds_would_mark_cancelled": 1,
  "prds_already_marked": 2, "automations_marked": 1, "left_cancelled": 1,
  "unmatched_ids": ["9d8e7f6a"],
  "errors": []
}
```

Run the real pass once the report looks right:

```
$ datawatch autonomous shipped backfill
```

MCP: `autonomous_shipped_backfill` (`dry_run` optional). Comm:

```
autonomous shipped-backfill dry_run=true
```

Two things worth remembering before you run it: it needs the repository
present on the machine that serves the daemon (a missing repository is a
400 on the run), and while `autonomous.shipped.enabled` is off it refuses
with the setting named. Turn `backfill_on_start` on if you want the same
pass to run at every daemon start.

## 7. What shipped does not do

- It does not change a stored status, ever — recording and backfill both
  leave it alone (the separate [Completed migration](automata-completed-migration.md) is what moves a merged cancelled record to `completed`), in either direction (a shipped completed stays
  `completed`; a shipped cancelled stays `cancelled`).
- It does not push, merge or move any branch: it records facts about
  merges that happened.
- It is not a per-record override: the settings are daemon-wide.
- It never fails a merge: the merge helper's recorded record is
  best-effort, and a failed record never fails a merge that succeeded.

## See also
- [Show finished Automata as Completed](automata-completed-migration.md) — the separate migration that moves a merged-but-cancelled record to `completed`

- [Autonomous planning](autonomous-planning.md) — the Automaton lifecycle the shipped state sits alongside
- [Automation chains](automation-chains.md) — the Automation records the shipped state covers
- [`config` reference: the `autonomous.shipped` block](../config-reference.yaml)
- [The shipped status definition](../datawatch-definitions.md)
- [Implementation notes](../implementation.md)
