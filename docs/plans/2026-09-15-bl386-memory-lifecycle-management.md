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
| `internal/mcp/memory_tools.go` | `memory_harvest`, `memory_handoff`, `memory_prd_report`, `memory_scope_inventory` tools |
| `internal/config/config.go` | `memory.sweep` TTL config block; `prd.memory_seed`/`memory_harvest` defaults |
| `cmd/datawatch/main.go` | Version → v8.30.0 |
| `internal/server/api.go` | Version → v8.30.0 |
| `CHANGELOG.md` | v8.30.0 entry |
| `README.md` | Badge + release entry |
| `docs/implementation.md` | New endpoints |

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

### Phase 2 — Harvest

1. `MemoryHarvestConfig` on `PRD` + story/task overrides.
2. Executor harvest on task completion.
3. `memory_harvest` MCP tool.
4. Tests:
   - `TestBL386_Executor_Harvests_OnTaskCompletion_WhenEnabled`
   - `TestBL386_Executor_Harvest_RoleFilter_Respected`
   - `TestBL386_Executor_NoHarvest_WhenDisabled`
   - `TestBL386_MemoryHarvest_Tool_ProxiesToScopeSeed`

### Phase 3 — Archive-on-delete

1. `memory_strategy` on `DeletePRD` + `DeleteSession`.
2. `archive` strategy: scope-seed to project-shared → scope-delete source.
3. PWA delete dialog selector.
4. Tests:
   - `TestBL386_DeletePRD_MemoryStrategy_Purge_DeletesScope`
   - `TestBL386_DeletePRD_MemoryStrategy_Archive_PromotesThenPurges`
   - `TestBL386_DeletePRD_MemoryStrategy_Keep_LeavesOrphans`
   - `TestBL386_DeleteSession_MemoryStrategy_Purge`
   - `TestBL386_DeleteSession_MemoryStrategy_Archive`

### Phase 4 — Handoff + PRD report

1. `memory_handoff` MCP tool.
2. `memory_prd_report` MCP tool + REST endpoint.
3. Tests:
   - `TestBL386_MemoryHandoff_WritesToStoryShared`
   - `TestBL386_MemoryPRDReport_AggregatesAllScopes`
   - `TestBL386_MemoryPRDReport_DeduplicatesAcrossScopes`

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
       └─ [BL385] scope_delete: purge source scope

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
