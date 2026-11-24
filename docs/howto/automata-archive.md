---
docs:
  index: true
  topics: [automata, archive, retention, retention window, backup, dry run, decision log, session history, task transcripts, performance, autonomous]
exec_steps:
  - tool: autonomous_archive
    description: Dry run. Report which finished Automata would be archived and the bytes before and after. Writes nothing
    args: {dry_run: true}
    read_only: true
  - tool: autonomous_prd_archive
    description: Read the archive of one archived Automaton, one request for one file. An Automaton that is not archived answers not found
    args: {id: a1b2c3d4}
    read_only: true
---

# How-to: archive finished Automata

A finished Automaton keeps a lot it no longer needs at hand: its decision log,
the session history of every task, the task transcripts (the raw judge and
verifier text) and long task errors.
The **archive** moves those parts out of the Automaton's live record into one
file of their own, so the daemon reads and holds far less for every Automaton
that is done. This page explains what is moved, what stays, how to read an
archive, how to run the archive yourself (dry run first, then apply), the backup
it takes, and the two settings that control it.

No data is deleted. The live record and the archive file together hold
everything the original held.

## What moves and what stays

| Moved to the archive file | Stays in the live record (the store) |
|---|---|
| The decision log | The summary: title, status, type, backend, priority, tags, links and timestamps |
| The session history of each task | The counts: stories, tasks and status counts, cost totals |
| The task transcripts: the raw judge and verifier text of every verdict and verification | The ids: the Automaton's, every story's and every task's |
| Long task errors (over 1 KiB) and the generated memory report | The statuses and the short fields of every story and task, plus an `archive` marker that names the archive file |

Because the live record keeps the summary, counts, ids and statuses, the lists,
the counts, the cost and the History read exactly as they did before. The
Automata list and the Projects roll-up come from the
[summary index](../datawatch-definitions.md#summary-index) and never needed the moved parts.
Opening an archived Automaton on the Automata tab is as quick as any other row;
the archive file is read only when you ask for it.

## When an Automaton is archived

An Automaton is archived when all of these hold:

- Its status is **completed**, **shipped** or **cancelled**.
- It was last updated longer ago than the **retention window**,
  `autonomous.archive.retention_days`.
- It is not skipped. An Automaton with a running task, an open session, or a
  completed record whose work branch is not merged is skipped and never forced.
  It is picked up on a later run once it qualifies.

A cancelled Automaton has no grace of its own in this batch archive: cancelling stamps its update time, so a freshly cancelled one is simply skipped as newer than the retention window and picked up once it is old enough. The `autonomous.archive_grace_minutes` grace is a separate thing — it delays only the supervisor loop's close step for a cancelled record (below), not this batch archive; see [operations.md](../operations.md) for it.

## The supervisor loop's guard on unmerged branch work

Separate from this batch archive, the supervisor loop's *close finished
records* step — which archives a completed, rejected or cancelled record on
its own schedule — now keeps **every one of those three states** while its
branch still holds commits that are not on the base branch, not just a
cancelled one. Completed, rejected and cancelled records are all kept while
the branch holds unmerged work; the held record and the reason ride on the
step's run record, which counts how many records it kept for unmerged branch
work, split by the state kept, and the close log line names the record and
the reason (the branch's commits ahead of base, or — when the check could
not compare — the reason it could not tell). The check is read-only and
conservative: a branch that is gone, or a branch with nothing ahead of base,
lets the record go; anything it cannot tell is kept. For completed and
rejected records you can release the guard with
`autonomous.archive_keep_unmerged: false` (unset reads `true`); a cancelled
record keeps its guard either way, and its grace is the
`autonomous.archive_grace_minutes` setting. Your own archive through this howto
is unaffected by either: it archives whenever the eligibility above holds.

The guard only keeps the record. It **never deletes a branch and never merges
one** — it performs no git write at all, only the read-only branch
comparison. A record stays until you archive it, its work is merged, or its
branch is deleted.

### Finding archived Automata that hold unmerged work

Once a record *is* archived — by this batch archive or by the loop after you
released its guard — the unmerged work is still on the branch. The
archived-unmerged finder lists every archived record whose branch still holds
commits that are not on the base branch (newest archive first), each with its
title, former status, branch and how many commits are ahead; a branch the
comparison could not make is listed with its reason. A branch whose work is
already on the base although no commit matches (a hand port) is left out: the
record's shipped record names a commit on the base, or every file the branch
changed has the same content there. It is a read — git is
only read, and it changes nothing, deletes nothing and merges nothing.

| Surface | How |
|---|---|
| REST | `GET /api/autonomous/archived-unmerged` |
| CLI | `datawatch autonomous archived-unmerged` (or `--json` for the body untouched) |
| MCP | `archived_unmerged_list` |
| Chat | `autonomous archived-unmerged` |

The supervisor watch raises the same work as the `archived_unmerged`
finding, so the Automata and Automation surfaces name it without you running
anything; see [automation-supervisor-watch.md](automation-supervisor-watch.md).

### Carrying one forward as a new draft

To keep the work instead of merging it, carry the archived record forward:
a new draft is created with the archived record's title, spec and branch,
linked back to it (its `carried_from_id`), and the reply gives you the new draft. It
is not approved and has run nothing; the archived record and its branch are
not changed, and nothing is merged or deleted. It is refused when the record
is not archived, carries no branch, the branch is gone or has nothing ahead
of base, the comparison could not be made, or the record was already carried
(pass `--force` to carry it a second time).

| Surface | How |
|---|---|
| REST | `POST /api/autonomous/archived-unmerged/{id}/carry-forward` (body `{force, actor}`) |
| CLI | `datawatch autonomous archived-unmerged carry-forward <id> [--force]` |
| MCP | `archived_unmerged_carry_forward` |
| Chat | `autonomous archived-unmerged carry-forward <id> [force]` |

The route and tool mapping is in [api-mcp-mapping.md](../api-mcp-mapping.md)
and the command reference is in [commands.md](../commands.md).
See [operations.md](../operations.md) (*Keep unmerged branch work on
archive*) and [the supervisor loop howto](supervisor-loop.md).

## Run the archive: dry run first, then apply

Always look before you move anything.

1. **Dry run.** It reports which Automata would be archived and the bytes before
   and after, and writes nothing. The dry run is the default.

   ```bash
   datawatch autonomous archive --dry-run
   ```

   The same report is available as `POST /api/autonomous/archive` with
   `{"dry_run": true}` (or no body), the MCP tool `autonomous_archive`, and the
   chat command `autonomous archive`.

2. **Apply.** When the report lists what you expect, run it for real.

   ```bash
   datawatch autonomous archive --apply
   ```

   The same through `POST /api/autonomous/archive` with `{"dry_run": false}`,
   and the MCP tool `autonomous_archive` with `dry_run` set to `false`.

`--dry-run` and `--apply` cannot be combined. A second apply finds nothing more
to do, so running it again is safe.

### The verified backup

An apply run first copies every record it is about to touch, the record file and
its side files, to `<data dir>/autonomous/archive-backups/<time>/`. It checks
each copy by size and SHA-256. If one copy does not verify, the run stops before
it changes anything. Only then does it archive one Automaton at a time, checking
each archive file before it rewrites the live record, so an interrupt leaves
every record at its old or its new state and never half way. Backups are kept;
delete them yourself once you are satisfied.

## Read an archive

Reading an archive returns what was moved out of one Automaton and touches no
other record.

| Surface | How |
|---|---|
| REST | `GET /api/autonomous/prds/{id}/archive` |
| CLI | `datawatch autonomous archive --id <id>` |
| MCP | `autonomous_prd_archive` with `id` |

An Automaton that is not archived answers **not found** (404). `--id` reads one
archive and takes neither `--dry-run` nor `--apply`.

The archive file itself is `<data dir>/autonomous/archive/<id>.json`. You do not
need to open it by hand.

## The settings

| Key | Default | What it does |
|---|---|---|
| `autonomous.archive.enabled` | `true` | Lets the daemon archive finished Automata on its own, at low frequency and never while an Automaton write is in progress. Set it to `false` and only `datawatch autonomous archive --apply` archives. |
| `autonomous.archive.retention_days` | `30` (a value of `0` means 30) | How many days old a finished Automaton must be before it is archived. A negative number is refused. |
| `autonomous.archive_keep_unmerged` | `true` | Read by the supervisor loop's close step, not by this batch archive: while `true` (the default) it keeps a completed or rejected Automaton whose branch holds commits that are not on the base branch. See above. |

Both change live, with no restart:

```bash
datawatch config set autonomous.archive.retention_days 60
datawatch config set autonomous.archive.enabled false
```

They are also settable from the web app (Settings, General, Autonomous), from
`PUT /api/config`, from the MCP `config_set` tool and from chat with
`configure`. The full table is in [operations.md](../operations.md) (search
"Archive settings").

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| The dry run lists nothing | No finished Automaton is older than the retention window, or the ones that are old are skipped (running task, open session, unmerged branch). Lower `autonomous.archive.retention_days` or finish the work first |
| An Automaton you expected is not in the report | It is not completed, shipped or cancelled yet, or it is still inside the retention window (a fresh cancel stamps the update time, so it counts as new) |
| `--id` answers "automaton is not archived" | That Automaton has not been archived. Run the dry run to see whether it qualifies |
| A finished Automaton the supervisor loop never closes | Its branch still holds commits that are not on the base branch (the loop keeps completed, rejected and cancelled records in that case). Merge the branch, delete it, or — for completed and rejected ones — set `autonomous.archive_keep_unmerged: false`; a cancelled one additionally needs its grace to have passed |
| The apply run stops with a backup error | One backup copy did not verify by size and SHA-256. Nothing was changed. Check free disk space and run it again |
| The daemon does not archive by itself | `autonomous.archive.enabled` is `false`. Set it to `true`, or apply by hand |

## Where to go next

- [automata-tab-loading.md](automata-tab-loading.md): how the Automata tab loads
  summaries first and why an archived row opens as fast as any other.
- [projects-tab.md](projects-tab.md): the Projects page, whose roll-up and
  History read the summary the archive keeps unchanged.
- [operations.md](../operations.md): the archive settings table and what the
  archive does, in the operator guide.
