---
docs:
  index: true
  topics: [memory, automata, scopes, lifecycle, harvest, archive, cross-prd, scope-recall]
exec_params:
  - {name: prd_id, required: true, description: "Automaton (PRD) ID to operate on"}
  - {name: project_dir, required: true, description: "Project directory"}
exec_steps:
  - tool: autonomous_prd_set_memory_seed
    description: Enable warm-start memory seeding on the Automaton
    args:
      id: "{{params.prd_id}}"
      enabled: true
      max_per_scope: 20
      role_filter: "learning,decision"
    read_only: false
  - tool: autonomous_prd_set_memory_harvest
    description: Enable memory harvest on task completion
    args:
      id: "{{params.prd_id}}"
      enabled: true
      promote_to: "story-shared"
      role_filter: "learning,decision"
    read_only: false
  - tool: autonomous_prd_get
    description: Read the Automaton memory report after a run
    args:
      id: "{{params.prd_id}}"
    read_only: true
---

# Automata Memory Workflow

This guide covers the complete memory lifecycle for Automata in datawatch: how to
configure memory seeding and harvest, how to use archive import to carry knowledge
forward, how to read an Automaton memory report, and how to use scope recall in MCP tools.

Prerequisite: v8.29.0 for scope model; v8.30.0 for lifecycle
policies; v8.31.0+ for Automaton orchestrator integration.

---

## Scope hierarchy

All memory operations in datawatch operate on a 6-layer scope hierarchy:

```
[1] persona-global         — across all projects, for this persona
[2] persona-in-project     — persona-specific within one project
[3] project-shared         — all sessions/PRDs in this project dir
[4] prd-shared      (NEW)  — all tasks within one PRD
[5] story-shared    (NEW)  — all tasks within one story
[6] session-local          — one session only (default write target)
```

Recall walks from [6] to [1] — most specific first.
Default write target (for subprocess mode) is [6] session-local.

**Subprocess mode:** When an AI tool (opencode, Goose, etc.) connects via MCP with
`--caller-session-id`, `--caller-prd-id`, and `--caller-story-id`, it automatically
routes writes to session-local and blocks destructive global operations like
`memory_sweep_stale`.

---

## Configuring memory seed on a PRD

Warm-start seeding pre-populates a new task session from applicable scopes before
the task begins work.

**Via MCP:**
```
autonomous prd set-memory-seed prd_id=<id> enabled=true max_per_scope=20 role_filter=learning,decision,pattern
```

**Via REST:**
```
PATCH /api/autonomous/prds/{id}/memory-seed
{
  "enabled": true,
  "max_per_scope": 20,
  "role_filter": ["learning", "decision", "pattern"]
}
```

**Via PRD YAML (template):**
```yaml
memory_seed:
  enabled: true
  max_per_scope: 20
  role_filter: ["learning", "decision", "pattern"]
```

When enabled, at task spawn time the executor seeds session-local from:
1. `project-shared` (project-wide learnings)
2. `prd-shared` (what this PRD has accumulated)
3. `story-shared` (what prior tasks in this story found)

---

## Cross-PRD seeding (from_prds)

A PRD can inherit memories from prior PRDs at first-task spawn time.

```
autonomous prd set-memory-seed prd_id=<id> enabled=true from_prds=<prd_a_id>,<prd_b_id>
```

This seeds the new PRD's `prd-shared` scope from each listed PRD's `prd-shared`
before the first task runs. Use this when PRD B is a follow-on to PRD A (e.g., a
research PRD followed by an implementation PRD).

**Distinct from archive import:** `from_prds` copies the live `prd-shared` scope of
an existing PRD; archive import imports from a deleted/completed PRD's archived
memories in `project-shared`.

---

## Configuring memory harvest

Harvest auto-promotes session-local learnings to a higher scope on task completion.

**Via MCP:**
```
autonomous prd set-memory-harvest prd_id=<id> enabled=true promote_to=story-shared role_filter=learning,decision max=50
```

**Via REST:**
```
PATCH /api/autonomous/prds/{id}/memory-harvest
{
  "enabled": true,
  "promote_to": "story-shared",
  "role_filter": ["learning", "decision"],
  "max": 50
}
```

`promote_to` options: `story-shared`, `prd-shared`, `project-shared`.

Use `story-shared` to share discoveries within a story's tasks; use `prd-shared`
to accumulate learnings across all stories in the PRD.

---

## Manual harvest (operator-triggered)

To harvest outside of automatic task completion:

```
memory harvest session_id=<id> project=<dir> promote_to=story-shared story_id=<id> role_filter=learning max=50 dry_run=true
```

Use `dry_run=true` first to see what would be promoted without writing.

---

## Task-to-task handoff

Explicitly write a structured handoff note to `story-shared` for the next task:

```
memory handoff summary="Found X works; Y fails at scale; use Z pattern for W" scope=story-shared role=handoff
```

The next task's warm-start seeding picks this up from `story-shared` automatically.

---

## Archive-on-delete: preserve knowledge before purging

When deleting a PRD, choose what to do with its memories:

**Via REST:**
```
DELETE /api/autonomous/prds/{id}
{
  "memory_strategy": "archive",
  "archive_role_filter": ["learning", "decision"],
  "archive_to_scope": "project-shared"
}
```

| Strategy | Behavior |
|----------|----------|
| `keep`   | Leave memories as orphaned project-shared (default; existing behavior) |
| `purge`  | Delete all memories in the entity's scope immediately |
| `archive`| Promote matching memories to `project-shared` with breadcrumb, then purge source scope |

The `archive` strategy adds `archived_from_prd: <prd_id>` breadcrumb to each
promoted memory, enabling future import with `memory_archive_import`.

---

## Archive import: seed a new PRD from a prior PRD's archived memories

After archiving a completed/deleted PRD's memories, seed a new PRD from those archives:

**Via MCP:**
```
memory archive-import project=<dir> source_prd_id=<old_id> target_prd_id=<new_id> role_filter=learning,decision max=30 dry_run=true
```

**Via REST:**
```
POST /api/memory/scopes/archive-import
{
  "project_dir": "/home/user/datawatch",
  "source_prd_id": "<old_prd_id>",
  "target_prd_id": "<new_prd_id>",
  "role_filter": ["learning", "decision"],
  "max": 30,
  "dry_run": false
}
```

Returns: `{"seeded": N, "skipped_duplicate": M, "dry_run": bool}`.

Deduplication: if the target PRD's `prd-shared` already contains a memory with the
same content, it is skipped (`skipped_duplicate`).

**Preset in PRD template:**
```yaml
memory_seed:
  from_archives:
    - prd_id: "<prior_prd_id>"
      role_filter: ["learning", "decision"]
      max: 20
```

---

## Reading a PRD memory report

After a PRD completes, a memory report is auto-generated and stored on the PRD record.

**Via MCP:**
```
autonomous prd get prd_id=<id>   # → .memory_report field (markdown)
```

**Via REST:**
```
GET /api/autonomous/prds/{id}    # → .memory_report, .memory_report_at
```

**Manual trigger / refresh:**
```
autonomous prd memory-report prd_id=<id>
# REST: POST /api/autonomous/prds/{id}/memory-report
```

The report aggregates `prd-shared` + `story-shared` memories, deduplicated and
grouped by scope. Format: markdown by default.

---

## Scope inventory: discovering what's in memory

List all scopes with data across a project:

**Via MCP:**
```
memory scope inventory project=<dir>
```

**Via REST:**
```
GET /api/memory/scopes/inventory?project=<dir>
```

Response groups by scope type: `prd-shared`, `story-shared`, `session-local`,
`project-shared` — with counts, oldest, newest timestamps per entity.

---

## Scope-aware TTL sweep

Configure different TTLs per scope layer in `~/.datawatch/config.yaml`:

```yaml
memory:
  sweep:
    session_local_days: 30
    story_shared_days: 90
    prd_shared_days: 180
    project_shared_days: 730
    auto: false   # set true to enable daily auto-sweep
```

Manual sweep:
```
memory sweep-stale scope=session-local older_than_days=30 dry_run=true
```

Omit `scope` for global sweep (backward-compatible with v8.29.x and earlier).

---

## How AI agents use scope recall (MCP tools)

When an AI tool runs in subprocess mode (e.g. opencode, Goose via datawatch MCP),
it automatically has the full hierarchy available:

```
# Session sees: session-local → story-shared → prd-shared → project-shared → persona layers
memory_recall query="what patterns were learned for this PRD?"
# Returns hits from all layers, most-specific first

# Write to prd-shared explicitly (available to all tasks in this PRD):
memory_remember scope=prd-shared text="The rate-limiter uses a sliding window; fixed-window causes thundering herd"

# Promote a discovery to story-shared for sibling tasks:
memory_scope_promote from_session=<session_id> memory_id=<id> to=story-shared story_id=<story_id>
```

The agent does not need to manage the hierarchy manually — `memory_recall` (no scope
arg) walks all 6 layers automatically. Only use explicit `scope=` writes when you
want to share beyond the default session-local boundary.

---

## Full lifecycle quick-reference

```
PRD created
  └─ set-memory-seed enabled=true [from_prds=...] [role_filter=...]
  └─ set-memory-harvest enabled=true promote_to=story-shared

PRD runs
  └─ Each task spawn: auto-seeded from project+prd+story scopes
  └─ Task work: writes go to session-local (default)
  └─ Task complete: harvest promotes learnings to story-shared / prd-shared

PRD complete
  └─ Auto-generated memory_report on PRD record
  └─ View: autonomous prd get prd_id=<id> → .memory_report

PRD deleted
  └─ DELETE with memory_strategy=archive → archives to project-shared
  └─ Breadcrumb: archived_from_prd=<id>

New PRD created
  └─ Optionally: memory archive-import source_prd_id=<old> target_prd_id=<new>
  └─ Or: set-memory-seed from_prds=<live_prd_id> for live cross-PRD seeding
```

---

## Troubleshooting

**No memories seeded at task spawn:** Check `memory_seed.enabled` on the PRD
(`autonomous prd get prd_id=<id>` → `.memory_seed.enabled`). Check that the source
scopes have data (`memory scope inventory project=<dir>`).

**Harvest not promoting:** Check `memory_harvest.enabled` and `promote_to`. Use
`memory harvest ... dry_run=true` to preview without writing.

**Archive import returns seeded=0:** Either the source PRD has no archived memories
(deleted with `purge` strategy, not `archive`), or all matching memories are already
in the target PRD's `prd-shared` scope (deduplicated). Use `dry_run=true` to inspect.

**Subprocess session writing to global scope:** Subprocess mode requires all three
caller flags: `--caller-session-id`, `--caller-prd-id`, `--caller-story-id`. Missing
flags = non-subprocess routing.
