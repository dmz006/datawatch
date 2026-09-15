# BL386 — Memory Lifecycle Management

**Status:** Draft  
**Target release:** v8.30.0 (minor; requires BL385 / v8.29.0 as foundation)  
**Filed:** 2026-09-15  
**Author:** dmz006

---

## Problem

Memory has no managed lifecycle. Sessions start cold, work in isolation, and leave
stale or orphaned memories behind. There is no:

- **Warm start** — new sessions begin with no relevant context even when prior work
  in the same PRD, story, or project is directly applicable
- **Live sharing** — parallel tasks in the same story/PRD have no convention for
  broadcasting discoveries to each other while running
- **Harvest** — when a task or session ends, useful memories aren't automatically
  surfaced upward; they stay session-local and age out
- **Archive** — when a PRD/session/story is deleted, memories are either orphaned
  forever or deleted entirely; there's no "promote the valuable ones before wiping"
- **Handoff** — task N in a story has no mechanism to deliberately pass context to
  task N+1
- **Report** — no "what did this PRD learn?" summary after completion

BL385 builds the scope model mechanics (new `prd-shared`/`story-shared` layers,
subprocess routing, `memory_scope_delete`, scope inventory). BL386 builds the
lifecycle policies on top of that foundation.

---

## Lifecycle model

A memory's lifecycle has five stages:

```
1. SEED     → new session/task pre-warmed from relevant scopes at spawn
2. WRITE    → session writes to session-local (default) or named scope during work
3. SHARE    → discoveries broadcast to story/prd scope while running (or on demand)
4. HARVEST  → on completion: valuable memories promoted upward; rest archived or left
5. ARCHIVE  → on entity delete: promote-then-purge vs direct-purge (operator choice)
```

Recall and sweep are maintenance operations that cut across all stages.

---

## Workflow diagram

```
PRD CREATED
  │
  ▼
DECOMPOSE (BL387)
  │  decomposer queries project-shared for prior context
  │  optionally auto-imports from_archives PRDs at PRD start
  ▼
TASK SPAWNED
  │
  ├─► [Phase 1] AUTO-SEED at spawn
  │     project-shared → session-local  (up to MaxPerScope)
  │     prd-shared     → session-local  (what this PRD learned so far)
  │     story-shared   → session-local  (what prior tasks in story found)
  │
  ▼
TASK RUNS
  │
  ├─► session-local writes  (default, BL385)
  ├─► explicit scope= writes to story/prd-shared  (BL385)
  ├─► memory_handoff: write summary to story-shared for next task  [Phase 4]
  └─► memory_recall: walks full 6-layer hierarchy  (BL385)
  │
  ▼
TASK COMPLETES
  │
  └─► [Phase 2] HARVEST
        session-local ─role_filter─► story-shared  (or prd-shared)
        breadcrumb: promoted_by=harvest, task_id, session_id
  │
  ▼
PRD COMPLETE
  │
  └─► [Phase 4] PRD MEMORY REPORT
        Aggregate prd-shared + story-shared + session-local
        Store on PRD.memory_report; surface in PWA tile (BL387)
  │
  ▼
PRD DELETED  (memory_strategy=archive | purge | keep)
  │
  ├─► archive: prd-shared ─role_filter─► project-shared
  │     breadcrumb: archived_from_prd=<id>, promoted_at
  │     then scope_delete prd-shared (BL385)
  │
  ├─► purge:  scope_delete prd-shared immediately
  │
  └─► keep:   memories remain as orphaned project-shared (current behavior)
  │
  ▼
NEW PRD CREATED
  │
  └─► [Phase 3] ARCHIVE IMPORT  (optional)
        query project-shared WHERE archived_from_prd=<source_id>
        seed into new PRD's prd-shared scope
        breadcrumb: imported_from_archive=<source_id>
        deduplicates against existing prd-shared memories

ROUTINE MAINTENANCE
  ├─► [Phase 5] Scope-aware TTL auto-sweep  (session-local 30d, prd 180d, ...)
  ├─► [Phase 5] memory_scope_inventory  (discover orphaned/large scopes)
  └─► memory_scope_inventory REST: GET /api/memory/scopes/inventory
```

---

## Feature 1 — Warm start: auto-seed at spawn

**What:** When the executor spawns a task session, it optionally pre-seeds
the new session with memories from applicable scopes before the task starts.

**Why:** Task sessions currently start with zero memory context. A task deep in a
PRD is unaware of what earlier tasks learned. Adding auto-seeding makes the scope
hierarchy actually useful — memories promoted by earlier tasks are automatically
visible to later ones without operator intervention.

### Executor changes

`SpawnRequest` gains optional seeding config:

```go
// SpawnRequest — new field (BL386)
MemorySeed MemorySeedConfig
```

```go
type MemorySeedConfig struct {
    Enabled     bool   // false = off (default for non-PRD sessions)
    MaxPerScope int    // cap per scope layer; default 20
    // Roles to include; empty = all roles.
    // e.g. ["learning", "pattern", "decision"]
    RoleFilter  []string
}
```

When `Enabled`, after the session is spawned and before it begins work, the executor
calls `POST /api/memory/scopes/seed` for each applicable scope in order:

1. `project-shared → session-local` (project-wide learnings)
2. `prd-shared → session-local` (what this PRD has accumulated)
3. `story-shared → session-local` (what earlier tasks in this story found)

Each seed call copies at most `MaxPerScope` memories matching `RoleFilter`, appending
a breadcrumb suffix so the session knows each memory is seeded, not native.

### PRD-level seeding config

PRDs gain a `memory_seed` config block:

```yaml
memory_seed:
  enabled: true         # default: false
  max_per_scope: 20
  role_filter: ["learning", "decision", "pattern"]
```

Exposed on all 5 PRD config surfaces (REST, MCP `autonomous_prd_set_llm` → new
`autonomous_prd_set_memory_seed` tool, CLI, comm channel, PWA).

### Non-PRD sessions

`start_session` gains an optional `memory_seed` param:

```json
{
  "task": "...",
  "project_dir": "...",
  "memory_seed": {
    "from_scope": "project-shared",
    "project":    "/home/user/datawatch",
    "max":        30,
    "role_filter": ["learning"]
  }
}
```

Allows operator-launched sessions to warm-start from any scope. Multiple
`memory_seed` entries supported (array) for seeding from multiple scopes.

---

## Feature 2 — Harvest: promote on task/session completion

**What:** After a task completes (reaches `completed` or `verifying` state), the
executor optionally promotes session-local memories matching criteria to
`story-shared` or `prd-shared`.

**Why:** Without harvest, all subprocess learnings stay session-local and are
invisible to sibling/successor tasks unless the operator manually promotes them.
Harvest makes the scope hierarchy self-filling.

### Harvest policy on PRD

PRDs (and individual stories/tasks) gain a `memory_harvest` config:

```yaml
memory_harvest:
  enabled: true             # default: false
  promote_to: story-shared  # or prd-shared, project-shared
  role_filter: ["learning", "decision"]
  max: 50
  # dry_run: true  — log what would be promoted, don't actually do it
```

When a task transitions to `completed`, the executor calls
`POST /api/memory/scopes/promote` (or bulk seed) for each matching memory,
moving it from `session-local` to the configured target scope. Breadcrumb records
`promoted_by: harvest`, `session`, `task_id`, `promoted_at`.

### Harvest tool (manual)

`memory_harvest` MCP tool for operator-triggered harvest outside the executor:

```
memory_harvest
  session_id=<id>
  project=<dir>
  promote_to=story-shared
  story_id=<id>      # or prd_id for prd-shared
  role_filter=learning,decision
  max=50
  dry_run=true
```

Wraps the existing `memory_scope_seed` call with a user-friendly interface and
dry-run mode.

---

## Feature 3 — Archive-on-delete: promote before purge

**What:** When a PRD, story, or session is deleted, memories are not simply orphaned
or silently purged. The operator chooses an archival strategy.

### Three strategies

| Strategy | Behavior |
|----------|----------|
| `keep` | Leave memories in place (current behavior). They become orphaned but remain searchable via direct scope recall. |
| `purge` | Delete all memories in the entity's scope immediately. Fast, irreversible. |
| `archive` | Promote memories matching a role filter to project-shared (with breadcrumb: "archived from prd/X"), then purge the source scope. Valuable memories survive; noise is deleted. |

### API changes

`DELETE /api/autonomous/prds/{id}` (or the existing cancel + delete path) gains:

```json
{
  "memory_strategy": "archive",   // keep | purge | archive; default: keep
  "archive_role_filter": ["learning", "decision"],  // used when strategy=archive
  "archive_to_scope": "project-shared"
}
```

`POST /api/sessions/delete` gains the same `memory_strategy` param.

### Default strategy

Default remains `keep` so existing behavior is unchanged. The PWA delete dialog
for PRDs surfaces a "What to do with memories?" selector.

### Archive import: seed a new PRD from archived memories

When memories are archived (promoted to project-shared with a breadcrumb recording
`archived_from_prd: <id>`), they become available for import into a new PRD or session.
This closes the feedback loop: knowledge from a completed or deleted PRD can be
deliberately carried forward into successor work.

**`memory_archive_import` MCP tool:**

```
memory_archive_import
  project=/home/user/datawatch
  source_prd_id=0fb4e302        # filter to archives from a specific PRD
  target_prd_id=a1b2c3d4        # seed this PRD's prd-shared scope
  role_filter=["learning","decision"]
  max=30
  dry_run=true                  # preview what would be seeded
```

**What it does:**
1. Queries `project-shared` for memories with breadcrumb `archived_from_prd=<source_prd_id>`
2. Seeds matching memories into the target PRD's `prd-shared` scope
3. Appends a breadcrumb: `"imported_from_archive: <source_prd_id>, imported_at: <ts>"`
4. Returns a summary: `{seeded: N, skipped_duplicate: M, dry_run: bool}`

**REST endpoint:**

```
POST /api/memory/scopes/archive-import
{
  "project_dir":    "/home/user/datawatch",
  "source_prd_id":  "0fb4e302",       // required
  "target_prd_id":  "a1b2c3d4",       // optional: if absent, seeds project-shared
  "role_filter":    ["learning"],      // optional
  "max":            30,                // optional; default 50
  "dry_run":        false
}
```

**PWA integration:** The "Create PRD" and "Instantiate PRD template" dialogs gain an
optional "Seed from prior PRD archives" multi-select, listing all PRDs that have
archived memories in the project. Selecting one or more fires `archive-import` before
the PRD starts. The `memory_seed.from_archives` config block on a PRD template
allows this to be preset:

```yaml
memory_seed:
  from_archives:
    - prd_id: "0fb4e302"
      role_filter: ["learning", "decision"]
      max: 20
```

This is distinct from BL387's `from_prds` cross-seeding — `from_prds` copies the
live prd-shared scope of an existing PRD; `from_archives` imports the archived
memories of a deleted or completed PRD that no longer has an active prd-shared scope.

---

## Feature 4 — Handoff: task-to-task context passing

**What:** A structured convention (not new code beyond BL385 scopes) for tasks in
a story to deliberately pass context to successor tasks.

**Convention:**

Task N writes its key discoveries to `story-shared` scope (via `memory_remember
scope=story-shared`). Task N+1 starts, hits `memory_recall` (which walks the
hierarchy including `story-shared`), and sees task N's discoveries naturally.

**Additional: explicit `memory_handoff` tool**

Wraps `memory_scope_promote` with a story-handoff-specific interface:

```
memory_handoff
  summary="Found that X approach works; Y approach fails at scale"
  scope=story-shared      # where to land (default: story-shared)
  role=handoff
  tags=["finding", "blocker"]  # future tagging (BL387)
```

Produces a single well-formatted memory in `story-shared` that the next task will
find at the top of its `story-shared` recall results (recency-ranked).

---

## Feature 5 — PRD memory report

**What:** After a PRD completes (or on-demand), generate a consolidated summary of
everything learned across all scopes associated with that PRD.

### `memory_prd_report` tool

```
memory_prd_report
  prd_id=<id>
  project=<dir>
  include_scopes=prd-shared,story-shared,session-local  # which scopes to include
  max_per_scope=50
  format=markdown    # markdown | json
```

**Behavior:**
1. Queries `prd-shared` for the PRD ID
2. Queries `story-shared` for each story in the PRD (from `prd.Story[]`)
3. Optionally queries `session-local` for each task's session (from task `session_id`)
4. Returns deduplicated, merged, grouped-by-scope results
5. In `format=markdown`: renders as a human-readable learning report

### REST endpoint

```
GET /api/autonomous/prds/{id}/memory-report
  ?include_scopes=prd-shared,story-shared
  &max_per_scope=50
  &format=markdown
```

---

## Feature 6 — Scope inventory and stats

*These were flagged in the BL385 gap analysis but belong here as lifecycle-management
tooling rather than scope-model mechanics.*

### `memory_scope_inventory`

List all scopes that have data, grouped by type:

```
memory_scope_inventory project=/home/user/datawatch

→ {
    "prd-shared": [
      {"prd_id": "0fb4e302", "count": 42, "oldest": "...", "newest": "..."},
      {"prd_id": "a1b2c3d4", "count": 8,  ...}
    ],
    "story-shared": [...],
    "session-local": [
      {"session_id": "johnnyjohnny-12cf", "count": 17, ...},
      ...
    ],
    "project-shared": {"count": 203, ...}
  }
```

Requires a new Backend query: `SELECT DISTINCT role, session_id, COUNT(*) FROM memories
WHERE project_dir=? GROUP BY role, session_id`. Non-breaking addition.

REST: `GET /api/memory/scopes/inventory?project=...`

### Per-scope stats in `memory_stats`

Extend the existing `GET /api/memory/stats` response with a `scopes` breakdown:

```json
{
  "total": 847,
  "project_shared": 203,
  "prd_shared": {"0fb4e302": 42, "a1b2c3d4": 8},
  "story_shared":  {"9b90b7a5": 15},
  "session_local": {"johnnyjohnny-12cf": 17, "...": 6}
}
```

---

## Feature 7 — Scope-aware TTL sweep

**What:** `memory_sweep_stale` currently takes `older_than_days` and sweeps the
entire global store. Scoped TTL lets different layers age at different rates.

### Config addition

```yaml
memory:
  sweep:
    session_local_days: 30     # session memories expire after 30 days
    story_shared_days:  90     # story memories expire after 90 days
    prd_shared_days:    180    # PRD memories expire after 180 days
    project_shared_days: 730   # project memories last 2 years
    # persona scopes follow existing sweep rules
```

### `memory_sweep_stale` extension

New optional `scope` param: `memory_sweep_stale scope=session-local older_than_days=30
dry_run=true`.

When `scope` is omitted, existing global sweep behavior is unchanged (backward-compat).

### Scheduled auto-sweep

Add a cron-style sweep task that runs daily using the config TTLs above. Off by
default; enabled via `memory.sweep.auto: true` config flag.

---

## Files to change

| File | Change |
|------|--------|
| `internal/autonomous/executor.go` | `MemorySeedConfig` on `SpawnRequest`; auto-seed at spawn; auto-harvest at task completion |
| `internal/autonomous/prd.go` (or store) | `MemorySeed`, `MemoryHarvest` fields on `PRD`; `memory_strategy` param on delete |
| `internal/autonomous/api.go` | `autonomous_prd_set_memory_seed`, `autonomous_prd_set_memory_harvest` tools; delete memory_strategy |
| `internal/server/api.go` | `memory_seed` on `handleStartSession`; `memory_strategy` on `handleDeleteSession` |
| `internal/server/memory_scopes.go` | `GET /api/memory/scopes/inventory`; `memory_prd_report` handler |
| `internal/server/server.go` | New routes; per-scope stats in `memory_stats`; scope TTL config; auto-sweep scheduler |
| `internal/memory/scopes.go` | Backend `Inventory()` query method signature (non-breaking add) |
| `internal/mcp/memory_tools.go` | `memory_harvest`, `memory_handoff`, `memory_prd_report`, `memory_scope_inventory`, `memory_archive_import` tools |
| `internal/config/config.go` | `memory.sweep` TTL config block; `prd.memory_seed`/`memory_harvest` defaults |
| `cmd/datawatch/main.go` | Version → v8.30.0 |
| `internal/server/api.go` | Version → v8.30.0 |
| `CHANGELOG.md` | v8.30.0 entry |
| `README.md` | Badge + release entry |
| `docs/implementation.md` | New endpoints |

---

## Comm-channel surface

All lifecycle tools are accessible across the full comm surface. Commands below use
the `memory` scope alias; the MCP tool names are the canonical form.

**Warm-start seed (Phase 1)**
```
memory scope seed project=<dir> from=project-shared to=session-local max=20
# MCP: memory_scope_seed source_scope=project-shared target_scope=session-local project=<dir> max=20

# Configure on PRD:
autonomous prd set-memory-seed prd_id=<id> enabled=true max_per_scope=20 role_filter=learning,decision
```

**Harvest (Phase 2)**
```
memory harvest session_id=<id> project=<dir> promote_to=story-shared story_id=<id> role_filter=learning max=50
# MCP: memory_harvest session_id=<id> project=<dir> promote_to=story-shared ...

# Configure on PRD:
autonomous prd set-memory-harvest prd_id=<id> enabled=true promote_to=story-shared role_filter=learning
```

**Handoff (Phase 4)**
```
memory handoff summary="Found X works, Y fails at scale" scope=story-shared role=handoff
# MCP: memory_handoff summary="..." scope=story-shared
```

**Archive import (Phase 3)**
```
memory archive-import project=<dir> source_prd_id=<id> target_prd_id=<id> role_filter=learning max=30
# MCP: memory_archive_import project=<dir> source_prd_id=<id> target_prd_id=<id> role_filter=learning

# REST:
POST /api/memory/scopes/archive-import
{"project_dir":"/home/user/datawatch","source_prd_id":"<id>","target_prd_id":"<id>","role_filter":["learning"],"max":30}
```

**PRD memory report (Phase 4)**
```
memory prd-report prd_id=<id> project=<dir> include_scopes=prd-shared,story-shared format=markdown
# MCP: memory_prd_report prd_id=<id> project=<dir>

# REST:
GET /api/autonomous/prds/{id}/memory-report?include_scopes=prd-shared,story-shared&format=markdown
```

**Scope inventory (Phase 5)**
```
memory scope inventory project=<dir>
# MCP: memory_scope_inventory project=<dir>

# REST:
GET /api/memory/scopes/inventory?project=<dir>
```

**Scope-aware TTL sweep (Phase 5)**
```
memory sweep-stale scope=session-local older_than_days=30 dry_run=true
# MCP: memory_sweep_stale scope=session-local older_than_days=30 dry_run=true
```

---

## Phase breakdown

### Phase 1 — Warm start seeding

1. `MemorySeedConfig` on `SpawnRequest`.
2. `memory_seed` config on `PRD` struct + setter tool.
3. Executor seeds at spawn (before session starts).
4. `start_session` `memory_seed` param.
5. Tests:
   - `TestBL386_Executor_AutoSeeds_FromProjectShared_WhenEnabled`
   - `TestBL386_Executor_AutoSeeds_FromPRDShared_WhenEnabled`
   - `TestBL386_Executor_AutoSeeds_FromStoryShared_WhenEnabled`
   - `TestBL386_Executor_NoSeed_WhenDisabled`
   - `TestBL386_StartSession_MemorySeed_ProxiesToScopeSeed`

**Phase 1 release checklist:**

- [ ] `MemorySeedConfig` on `SpawnRequest` — zero-value = disabled (no behavior change)
- [ ] PRD `memory_seed` config block + `autonomous_prd_set_memory_seed` MCP tool
- [ ] Executor seeds at spawn when `memory_seed.enabled=true`
- [ ] `start_session` `memory_seed` param wired
- [ ] All 5 Phase 1 tests pass: `rtk go test ./internal/autonomous/...`
- [ ] `docs/testing-tracker.md`: Phase 1 row added, Tested=Yes
- [ ] Backward compat: non-PRD sessions unaffected; `memory_seed.enabled` defaults false

### Phase 2 — Harvest

1. `MemoryHarvestConfig` on `PRD` + story/task overrides.
2. Executor harvest on task completion.
3. `memory_harvest` MCP tool.
4. Tests:
   - `TestBL386_Executor_Harvests_OnTaskCompletion_WhenEnabled`
   - `TestBL386_Executor_Harvest_RoleFilter_Respected`
   - `TestBL386_Executor_NoHarvest_WhenDisabled`
   - `TestBL386_MemoryHarvest_Tool_ProxiesToScopeSeed`

**Phase 2 release checklist:**

- [ ] `MemoryHarvestConfig` on `PRD` struct + story/task override support
- [ ] Executor harvest hook on task `completed` transition
- [ ] `memory_harvest` MCP tool with dry_run support
- [ ] `autonomous_prd_set_memory_harvest` config tool
- [ ] All 4 Phase 2 tests pass
- [ ] `docs/testing-tracker.md`: Phase 2 row = Tested=Yes
- [ ] fedCap guard: `memory_harvest` write endpoint requires `fedCap.MemoryWrite` on federated peers

### Phase 3 — Archive-on-delete + archive import

1. `memory_strategy` on `DeletePRD` + `DeleteSession`.
2. `archive` strategy: scope-seed to project-shared → scope-delete source.
3. Breadcrumb `archived_from_prd` on each archived memory.
4. `POST /api/memory/scopes/archive-import` REST endpoint.
5. `memory_archive_import` MCP tool.
6. `memory_seed.from_archives` config on PRD struct + setter.
7. PWA delete dialog selector: "What to do with memories?"
8. PWA PRD creation: "Seed from prior PRD archives" multi-select.
9. Tests:
   - `TestBL386_DeletePRD_MemoryStrategy_Purge_DeletesScope`
   - `TestBL386_DeletePRD_MemoryStrategy_Archive_PromotesThenPurges`
   - `TestBL386_DeletePRD_MemoryStrategy_Keep_LeavesOrphans`
   - `TestBL386_DeleteSession_MemoryStrategy_Purge`
   - `TestBL386_DeleteSession_MemoryStrategy_Archive`
   - `TestBL386_ArchiveImport_SeedsFromBreadcrumbedArchive`
   - `TestBL386_ArchiveImport_DryRun_DoesNotWrite`
   - `TestBL386_ArchiveImport_DeduplicatesExistingScoped`
   - `TestBL386_PRD_FromArchives_Config_AutoImportsAtSpawn`

**Phase 3 release checklist:**

- [ ] `memory_strategy` param on `DELETE /api/autonomous/prds/{id}` and `DELETE /api/sessions/{id}`
- [ ] `archive` strategy: scope-seed to project-shared → scope_delete source scope
- [ ] `archived_from_prd` breadcrumb on each archived memory
- [ ] `POST /api/memory/scopes/archive-import` REST endpoint
- [ ] `memory_archive_import` MCP tool (with `dry_run` support)
- [ ] `memory_seed.from_archives` config on PRD struct + setter
- [ ] PWA delete dialog: "What to do with memories?" selector (keep/purge/archive)
- [ ] PWA PRD creation: "Seed from prior PRD archives" multi-select
- [ ] All 9 Phase 3 tests pass (5 delete-strategy + 4 archive-import)
- [ ] `docs/testing-tracker.md`: Phase 3 row = Tested=Yes
- [ ] fedCap guard on `POST /api/memory/scopes/archive-import` (autonomous write endpoint)
- [ ] **mobile-parity: datawatch-app#175** — Android PRD delete screen: `memory_strategy` selector; PRD create/edit: `from_archives` multi-select; locale keys below
- [ ] Locale keys (add to all 5 locale bundles + datawatch-app#175):
  - `memory_strategy_keep` — "Keep memories (orphaned)"
  - `memory_strategy_purge` — "Delete all memories"
  - `memory_strategy_archive` — "Archive valuable memories"
  - `memory_seed_enabled` — "Warm-start seeding"
  - `memory_harvest_enabled` — "Auto-harvest on completion"
  - `prd_memory_report` — "PRD Memory Report"
  - `memory_from_archives` — "Seed from prior archives"

### Phase 4 — Handoff + PRD report

1. `memory_handoff` MCP tool.
2. `memory_prd_report` MCP tool + REST endpoint.
3. Tests:
   - `TestBL386_MemoryHandoff_WritesToStoryShared`
   - `TestBL386_MemoryPRDReport_AggregatesAllScopes`
   - `TestBL386_MemoryPRDReport_DeduplicatesAcrossScopes`

**Phase 4 release checklist:**

- [ ] `memory_handoff` MCP tool writes to `story-shared` with `role=handoff`
- [ ] `memory_prd_report` MCP tool: aggregates prd-shared + story-shared + session-local
- [ ] `GET /api/autonomous/prds/{id}/memory-report` REST endpoint
- [ ] All 3 Phase 4 tests pass
- [ ] `docs/testing-tracker.md`: Phase 4 row = Tested=Yes

### Phase 5 — Inventory, per-scope stats, scope TTL

1. Backend `Inventory()` query method.
2. `memory_scope_inventory` tool + REST endpoint.
3. Per-scope breakdown in `memory_stats`.
4. `memory.sweep` TTL config; `memory_sweep_stale scope=` param.
5. Auto-sweep scheduler (off by default).
6. Tests:
   - `TestBL386_ScopeInventory_ReturnsAllScopes`
   - `TestBL386_MemoryStats_IncludesScopeBreakdown`
   - `TestBL386_ScopedSweep_SessionLocal_OnlySweesScopeSessionLocal`
   - `TestBL386_ScopedSweep_GlobalFallback_WhenNoScopeProvided`

**Phase 5 release checklist:**

- [ ] Backend `Inventory()` query method (non-breaking additive SQL query)
- [ ] `memory_scope_inventory` MCP tool + `GET /api/memory/scopes/inventory` REST
- [ ] Per-scope breakdown added to `GET /api/memory/stats` response (additive, backward-compat)
- [ ] `memory.sweep` TTL config block (session_local/story/prd/project days)
- [ ] `memory_sweep_stale scope=` param (omit = global sweep unchanged)
- [ ] Auto-sweep cron scheduler (disabled by default; `memory.sweep.auto: true` to enable)
- [ ] All 4 Phase 5 tests pass
- [ ] `docs/testing-tracker.md`: Phase 5 row = Tested=Yes

---

## v8.30.0 Full Release Checklist

- [ ] All Phases 1–5 complete and tests pass
- [ ] `rtk go test ./...` — zero failures
- [ ] ZAP scan: no new WARN or FAIL on new endpoints (archive-import, inventory, memory-report)
- [ ] `scripts/release-smoke.sh` — passes
- [ ] Both version files bumped to `v8.30.0`:
  - `cmd/datawatch/main.go`
  - `internal/server/api.go`
- [ ] `CHANGELOG.md`: v8.30.0 entry with all 7 features documented
- [ ] `README.md`: badge + release entry updated
- [ ] `docs/implementation.md`: all new REST endpoints documented
- [ ] `docs/howto/prd-memory-workflow.md`: warm-start/harvest/archive/import workflow guide
- [ ] `docs/datawatch-definitions.md`: warm-start seeding, memory harvest, archive-on-delete, archive import, memory handoff, PRD memory report definitions added
- [ ] `docs/parity-status.md`: lifecycle row (PWA ✓ | Android datawatch-app#175 | iOS datawatch-app#175)
- [ ] `mobile-parity: datawatch-app#175 filed ✓`
- [ ] fedCap guards on all autonomous write paths verified
- [ ] **Do NOT** run `make cross` or `gh release create` manually — tag `v8.30.0` → CI handles release

---

## E2e test scenarios (`internal/mcp/bl386_e2e_test.go`)

In-process daemon with real SQLite memory backend + fake LLM. Requires BL385 e2e suite passing first.

**E1: Warm-start seed + harvest round-trip**
```
1. Create PRD with memory_seed.enabled=true, memory_harvest.enabled=true,
   memory_harvest.promote_to=story-shared
2. Write 5 project-shared memories manually (simulating prior work)
3. Executor spawns task → assert session-local has those 5 seeded memories
4. Task writes 3 new session-local memories (role=learning)
5. Task completes → assert story-shared has 3 harvested memories
6. Second task in same story spawns → assert session-local seeded from story-shared
   (task 1's harvest is visible to task 2)
```

**E2: Archive-on-delete + scope cleanup**
```
1. PRD with 10 prd-shared memories (roles: learning=6, noise=4)
2. Delete PRD with memory_strategy=archive, archive_role_filter=[learning]
3. Assert: prd-shared count = 0
4. Assert: project-shared count increased by 6
5. Assert: breadcrumb archived_from_prd=<prd_id> on all 6 archived memories
6. Assert: noise memories gone (not in any scope)
```

**E3: Archive import — knowledge transfer to new PRD**
```
1. Run E2 first (or set up same state)
2. Create new PRD B
3. Call memory_archive_import source_prd_id=<old_id> target_prd_id=<B_id>
   role_filter=[learning] max=10
4. Assert: B's prd-shared has 6 memories (imported_from_archive breadcrumb)
5. Call archive_import again → assert skipped_duplicate=6, seeded=0
6. Verify dry_run=true returns count=6 without writing
```

**E4: PRD memory report**
```
1. PRD with 5 prd-shared + 3 story-shared + 2 session-local memories across 2 tasks
2. GET /api/autonomous/prds/{id}/memory-report?include_scopes=prd-shared,story-shared
3. Assert: response contains all 8 memories (5+3), grouped by scope
4. Assert: session-local excluded when not in include_scopes
5. Assert: format=markdown returns human-readable output
```

**E5: Scope-aware TTL sweep**
```
1. Write 4 session-local memories timestamped 31 days ago (test hook)
2. Write 2 prd-shared memories timestamped 31 days ago
3. Config: session_local_days=30, prd_shared_days=180
4. Call memory_sweep_stale scope=session-local older_than_days=30
5. Assert: 4 session-local deleted
6. Assert: 2 prd-shared NOT deleted (prd TTL=180 not exceeded)
7. Call memory_sweep_stale (no scope) → assert global sweep still works (backward compat)
```

**E2e checklist:**

- [ ] `internal/mcp/bl386_e2e_test.go` created with all 5 scenarios
- [ ] All 5 pass with BL385 + BL386 in-process
- [ ] Comm-channel commands verified via `POST /api/test/message` (e.g. `memory archive-import ...`)
- [ ] `docs/testing-tracker.md`: BL386 e2e section added, all 5 = Tested=Yes
- [ ] E1 and E3 marked Validated=Yes after live daemon run

---

## Relationship to BL385

BL385 is the prerequisite — it provides:
- `prd-shared` and `story-shared` scope layers
- `POST /api/memory/scopes/save` (scoped write)
- `DELETE /api/memory/scopes/delete` (bulk scope delete, needed by archive-on-delete)
- Subprocess auto-routing to session-local
- `--caller-prd-id`, `--caller-story-id` executor injection

BL386 builds policies on top — no BL386 phase is shippable without BL385.

---

## Summary: full lifecycle picture

```
Task spawns
  └─ [BL386 Phase 1] auto-seed from project/prd/story scopes
       └─ task runs
            ├─ [BL385] writes go to session-local by default
            ├─ [BL385] explicit scope= writes to story/prd-shared
            ├─ [BL385] recall walks full hierarchy
            └─ task completes
                 ├─ [BL386 Phase 2] harvest: promote learnings to story/prd-shared
                 └─ [BL386 Phase 4] handoff: write summary for next task

PRD/session deleted
  └─ [BL386 Phase 3] archive: promote valuable memories → project-shared
       ├─ [BL385] scope_delete: purge source scope
       └─ [BL386 Phase 3] archived memories tagged with archived_from_prd breadcrumb

New PRD/session created
  └─ [BL386 Phase 3] archive import: seed prd-shared from archived memories of prior PRD
       └─ memory_archive_import from_archives=[prior_prd_id] → prd-shared

Routine maintenance
  ├─ [BL386 Phase 5] scope TTL auto-sweep (session-local 30d, prd 180d, ...)
  ├─ [BL386 Phase 5] scope inventory: discover orphaned/large scopes
  └─ [BL386 Phase 4] PRD report: "what did this PRD learn?"
```

---

## Out of scope (BL386)

- Auto-promote policy engine (declarative rules) — separate future item
- Memory tagging / labels beyond role field — separate BL387
- Cross-scope deduplication at promote/seed time — BL385 Phase 2
- Memory quota / per-scope size limits — separate future item
- PWA scope browser / memory explorer — follow-on UI work
