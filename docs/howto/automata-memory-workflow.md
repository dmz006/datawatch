---
docs:
  index: true
  topics: [memory, prd, autonomous, scopes, lifecycle, harvest, archive, subprocess, cross-prd]
exec_params:
  - {name: prd_id, required: true, description: "PRD ID to operate on"}
  - {name: project_dir, required: true, description: "Project directory"}
exec_steps:
  - tool: autonomous_prd_set_memory_seed
    description: Enable warm-start seeding on the PRD
    args:
      id: "{{params.prd_id}}"
      enabled: true
      max_per_scope: 20
      role_filter: "learning,decision"
    read_only: false
  - tool: autonomous_prd_set_memory_harvest
    description: Enable harvest on task completion
    args:
      id: "{{params.prd_id}}"
      enabled: true
      promote_to: "story-shared"
      role_filter: "learning,decision"
    read_only: false
  - tool: autonomous_prd_run
    description: Run the PRD
    args:
      id: "{{params.prd_id}}"
    read_only: false
---
# How-to: Automata memory workflow

A complete guide to using memory across the Automata lifecycle — from
decomposition through task execution to archiving. Covers all 6 scope
layers, automatic seeding and harvest, cross-Automata knowledge transfer,
and the archive-import pattern for building on prior work.

**Target audience:** AI agents using MCP tools to manage Automata, and
human operators setting up autonomous workflows.

## When to use this

- You are spawning or managing an Automaton via MCP tools and want tasks to
  share discoveries with each other automatically
- You want a new Automaton to benefit from what a prior Automaton learned
- You are an AI agent in a subprocess task session and want to write
  memories that persist beyond your session
- You want to carry institutional knowledge forward when deleting a
  completed Automaton

---

## Quick-start (for an AI agent in a running task session)

If you are already running inside a subprocess task session (the
executor spawned you), your `callerSessionID`, `callerPRDID`, and
`callerStoryID` are already set. Use them:

```
# Write a learning that ALL tasks in this story will see:
memory_remember
  scope=story-shared
  story_id=<your_story_id>
  project=<project_dir>
  text="Found that approach X works; Y fails at scale because of Z"
  role=learning

# Write a finding that retry tasks of this PRD will see:
memory_remember
  scope=prd-shared
  prd_id=<your_prd_id>
  project=<project_dir>
  text="API rate-limit hit on /v2/items — add exponential backoff with jitter"
  role=verifier-finding

# Recall everything relevant to your task (walks all 6 layers):
memory_recall
  query="rate limit retry patterns"
  project=<project_dir>

# Leave a structured handoff for the next task in this story:
memory_handoff
  summary="Completed auth module; left stub in handlers/auth.go:245 for token refresh"
  scope=story-shared
  role=handoff
```

You do not need to pass `scope=` for ordinary notes — `memory_remember`
with no scope writes to session-local by default. Only use explicit
scopes when you want a memory to outlive your session.

---

## Architecture: the 6-layer scope hierarchy

```
┌─────────────────────────────────────────────────────────────┐
│  [1] persona-global     (all projects, this persona)        │
│    └─ [2] persona-in-project  (this project, this persona)  │
│         └─ [3] project-shared  (all sessions in project)    │
│              └─ [4] prd-shared  (all tasks in this PRD) ◄── NEW
│                   └─ [5] story-shared  (all tasks in story)◄NEW
│                        └─ [6] session-local  (this task)    │
└─────────────────────────────────────────────────────────────┘

Recall walks [6] → [1], merging results.
Default write in subprocess mode → [6] session-local.
```

### Scope reference table

| Scope | Shared between | Write via | Survives delete? |
|-------|---------------|-----------|-----------------|
| `session-local` | This task only | Default in subprocess | No (unless archived) |
| `story-shared` | All tasks in one story | `scope=story-shared` | No (unless archived) |
| `prd-shared` | All tasks in one PRD | `scope=prd-shared` | No (unless archived) |
| `project-shared` | All sessions in project | `scope=project-shared` | Yes |
| `persona-in-project` | All sessions as this persona in project | `scope=persona-in-project` | Yes |
| `persona-global` | All sessions as this persona | `scope=persona-global` | Yes |

---

## Setting up a PRD with memory enabled

### Full MCP flow (operator or AI managing a PRD)

```
# 1. Create the PRD:
autonomous_prd_create
  title="Implement user auth module"
  spec="..."
  project_dir=<project_dir>

# 2. Enable warm-start seeding (tasks pre-warmed from higher scopes):
autonomous_prd_set_memory_seed
  id=<prd_id>
  enabled=true
  max_per_scope=20
  role_filter=learning,decision,pattern

# 3. Enable harvest on task completion (session learnings promoted up):
autonomous_prd_set_memory_harvest
  id=<prd_id>
  enabled=true
  promote_to=story-shared
  role_filter=learning,decision
  max=50

# 4. (Optional) Seed from a prior PRD's archived memories:
memory_archive_import
  project=<project_dir>
  source_prd_id=<old_prd_id>
  target_prd_id=<prd_id>
  role_filter=learning
  dry_run=true
# Review output, then run without dry_run:
memory_archive_import
  project=<project_dir>
  source_prd_id=<old_prd_id>
  target_prd_id=<prd_id>
  role_filter=learning

# 5. (Optional) Seed from a live PRD's prd-shared scope:
autonomous_prd_set_memory_seed
  id=<prd_id>
  from_prds=<related_prd_id>

# 6. Run:
autonomous_prd_run id=<prd_id>

# 7. After completion — generate a learning report:
memory_prd_report
  prd_id=<prd_id>
  project=<project_dir>
  format=markdown
```

### Via comm channel

```
autonomous prd create title="..." spec="..." project=<dir>
autonomous prd set-memory-seed <id> enabled=true max_per_scope=20 role_filter=learning,decision
autonomous prd set-memory-harvest <id> enabled=true promote_to=story-shared role_filter=learning,decision
memory archive-import project=<dir> source_prd_id=<old> target_prd_id=<id> role_filter=learning dry_run=true
autonomous prd run <id>
memory prd-report prd_id=<id> project=<dir> format=markdown
```

---

## What happens automatically (when seed + harvest enabled)

When `memory_seed.enabled=true` and `memory_harvest.enabled=true` on a
PRD, the executor manages memory at every lifecycle stage without
operator intervention:

1. **Task spawned** — executor seeds the new session-local from:
   - `project-shared` (project-wide learnings, up to `max_per_scope`)
   - `prd-shared` (what earlier tasks in this PRD found, including verifier findings)
   - `story-shared` (what earlier tasks in this story found)

2. **Task runs** — subprocess session writes to session-local by default.
   Agent can also write explicitly to `story-shared` or `prd-shared`.

3. **Task fails verification** — verifier writes structured findings to
   `prd-shared` with `role=verifier-finding`. Next retry task sees them
   in its seed (step 1 above).

4. **Task completes** — executor harvests session-local memories matching
   `role_filter` → `story-shared` (or `prd-shared` if configured). Next
   task in the story starts pre-warmed with these discoveries.

5. **PRD completes** — auto-generated `memory_prd_report` stored on the
   PRD record (`memory_report` field). Scope inventory updated.

6. **PRD deleted with `memory_strategy=archive`** — qualifying memories
   promoted to `project-shared` with breadcrumb `archived_from_prd=<id>`,
   then `prd-shared` and `story-shared` scopes purged.

7. **New PRD created** — can import archived memories via
   `memory_seed.from_archives` config or the `memory_archive_import` tool.

---

## PRD lifecycle diagram

```
PRD CREATED
  │
  ├─ memory_seed.from_archives=[prior_id]  ←── import archived memories
  ├─ memory_seed.from_prds=[live_id]       ←── inherit live prd-shared
  │
  ▼
DECOMPOSE
  │  Decomposer queries project-shared → injects top-15 as prior-context
  ▼
STORY 1
  ├─ TASK 1.1 spawned
  │    └─ [auto-seed] project-shared + prd-shared + story-shared → session-local
  │    └─ task runs → writes session-local + explicit scopes
  │    └─ [harvest] session-local learning → story-shared
  │
  ├─ TASK 1.2 spawned (sees task 1.1's story-shared memories)
  │    └─ [auto-seed] + task 1.1's harvested memories in story-shared
  │    └─ ...
  │
STORY 2
  ├─ TASK 2.1 spawned (sees story 1's prd-shared memories)
  │    └─ [auto-seed] prd-shared includes learnings from story 1
  │    └─ ...

ALL COMPLETE
  ├─ [auto-report] memory_prd_report generated
  └─ PRD deleted (strategy=archive)
       ├─ learnings → project-shared
       └─ prd-shared + story-shared purged

NEW PRD
  └─ memory_archive_import source_prd_id=<old> → new prd-shared
```

---

## Verifier feedback loop

When a task fails verification, retries automatically see the failure reason:

```
Task fails verification
  └─ Verifier writes to prd-shared:
       role=verifier-finding
       text="Missing null check in handler.go:45; test TestHandlerNil fails"

Task retried (auto-seeded from prd-shared)
  └─ Session sees: "Verifier finding: Missing null check..."
  └─ Session fixes the issue
  └─ Task passes verification
  └─ No new verifier-findings → harvest skips role=verifier-finding
```

No operator intervention needed. The verifier-finding memories persist
in `prd-shared` until the PRD is deleted or archived.

---

## Cross-PRD knowledge transfer

### `from_prds` — live scope inheritance

Inherit memories from a currently-running or completed (but not deleted)
PRD's `prd-shared` scope:

```
autonomous_prd_set_memory_seed
  id=<new_prd_id>
  from_prds=<related_prd_id_a>,<related_prd_id_b>
```

The new PRD's decomposer and task sessions see those PRDs' accumulated
learnings. Used when PRDs are sequentially related (Part 1 → Part 2).

### `from_archives` — archived memories of deleted PRDs

Inherit from a prior PRD that was archived when deleted:

```yaml
# In PRD config (set via autonomous_prd_set_memory_seed):
memory_seed:
  from_archives:
    - prd_id: "0fb4e302"
      role_filter: ["learning", "decision"]
      max: 20
```

Or manually:

```
memory_archive_import
  project=<dir>
  source_prd_id=0fb4e302
  target_prd_id=<new_id>
  role_filter=learning,decision
  dry_run=true   # preview first
```

### Child PRD inheritance (automatic)

When a task spawns a child PRD (via `autonomous_prd_create` from within
a task session), the child inherits the parent's `prd-shared` memories
automatically at instantiation — no config needed.

---

## Archive and delete strategies

When deleting a PRD, choose what happens to its memories:

| Strategy | Command | When to use |
|----------|---------|-------------|
| `keep` (default) | `autonomous prd delete <id>` | Memories stay as orphaned scopes; searchable but no longer part of any hierarchy |
| `purge` | `autonomous prd delete <id> memory_strategy=purge` | Hard delete all memories; irreversible |
| `archive` | `autonomous prd delete <id> memory_strategy=archive archive_role_filter=learning,decision` | Promote qualifying memories to project-shared, then purge scopes |

**Archive is recommended** for any PRD that ran successfully — it
preserves institutional knowledge in project-shared where all future
PRDs and sessions can see it.

After archiving, the memories carry a breadcrumb:
```
_(archived from prd/0fb4e302 at 2026-09-15T14:22:00Z)_
```

A new PRD can import them with `memory_archive_import`.

---

## Checking scope state

```
# What scopes have data in this project?
memory_scope_inventory project=<dir>
→ {
    "prd-shared":    [{"prd_id": "0fb4e302", "count": 42, "newest": "..."}],
    "story-shared":  [{"story_id": "9b90b7a5", "count": 15, ...}],
    "session-local": [{"session_id": "abc-123", "count": 7, ...}],
    "project-shared": {"count": 203}
  }

# Per-scope breakdown in memory stats:
memory_stats project=<dir>
→ includes "scopes": {"prd_shared": {"0fb4e302": 42}, "story_shared": {...}, ...}
```

---

## Troubleshooting

**"My memories aren't visible to the next task"**
→ Check `memory_seed.enabled=true` and `memory_harvest.enabled=true` on the PRD.
→ Check `role_filter` — if it's set to `["learning"]` but the task only wrote
   `role=finding`, the harvest skips it. Use `memory_scope_inventory` to see counts.

**"I wrote to prd-shared but recall doesn't return it"**
→ Verify the `prd_id` you passed matches the PRD the session was spawned for.
→ Run `memory_scope_inventory project=<dir>` to confirm the scope has entries.
→ Check the recall query — semantic search may not match; try a broader query.

**"archive-import returned seeded=0"**
→ Check the source PRD was deleted with `memory_strategy=archive`, not `purge` or `keep`.
→ Confirm `role_filter` matches roles that were actually written. Check with
   `memory_scope_inventory` before and after archive — project-shared count should have increased.

**"The decomposer isn't using project-shared memories"**
→ Decomposer enrichment requires v8.32.0+. Verify version with `get_version`.
→ Check project-shared has entries: `memory_scope_inventory project=<dir>`.

**"Subprocess session can write to global scope"**
→ Should not happen. In subprocess mode, `memory_remember` with no `scope=` always
   goes to session-local. If you see global writes, check that `--caller-session-id`
   was passed at spawn time (`subprocessMode()` requires it).

---

## Base requirements

- datawatch v8.29.0+ for 6-layer scope model
- datawatch v8.30.0+ for lifecycle management — seeding, harvest, archive
- datawatch v8.31.0+ for verifier feedback, child PRD inheritance
- datawatch v8.32.0+ for decomposer enrichment, cross-PRD seeding
- Memory backend + embedder configured (see [cross-agent-memory.md](cross-agent-memory.md))

---

## See also

- [howto/cross-agent-memory.md](cross-agent-memory.md) — full scope mechanics, borrow/seed/promote
- [howto/autonomous-planning.md](autonomous-planning.md) — PRD creation and decomposition
- [howto/autonomous-review-approve.md](autonomous-review-approve.md) — guided mode and approval flow
- [datawatch-definitions.md](../datawatch-definitions.md) — scope, lifecycle, harvest, archive definitions
