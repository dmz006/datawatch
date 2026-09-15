# BL387 — PRD Memory Integration

**Status:** Draft  
**Target releases:** v8.31.0 (Phase 1) · v8.32.0 (Phase 2) · v8.33.0 (Phase 3)  
**Prerequisites:** BL385 (v8.29.0) · BL386 (v8.30.0)  
**Filed:** 2026-09-15  
**Author:** dmz006

---

## Overview

BL385 builds the scope model. BL386 builds lifecycle policies (seeding, harvest,
archive, report). BL387 integrates those capabilities directly into the PRD
orchestrator — decomposer, verifier, executor, and PWA — so the memory system
actively improves PRD execution quality rather than sitting as a passive store.

**Six features across three releases:**

| Phase | Release | Features |
|-------|---------|---------|
| 1 | v8.31.0 | Verifier memory; child PRD inheritance |
| 2 | v8.32.0 | Decomposer enrichment; cross-PRD seeding |
| 3 | v8.33.0 | Auto-report on completion; memory stats PWA card |

After Phase 3 all three BLs (385–387) form the complete memory lifecycle and
gate the v9.0.0 major release.

---

## Workflow diagram

```
PRD LIFECYCLE WITH MEMORY INTEGRATION (BL387 overlay on BL385+386 foundation)

PRD CREATED
  │
  ├─► [Phase 2b] from_prds=[A,B]: seed prd-shared from prior PRD scopes at first spawn
  │
  ▼
DECOMPOSE  [Phase 2a]
  │
  ├─► Query project-shared (top-15 hits, semantic match against PRD spec)
  │     Inject as "## Prior context from project memory" block in decomposer prompt
  │     → avoid repeating known dead-ends; use proven patterns
  │
  ▼
TASK SPAWNED
  │
  ├─► [BL386 Phase 1] Auto-seed: project-shared + prd-shared + story-shared → session-local
  │     role_filter includes "verifier-finding" (Phase 1a) when seed enabled
  │
  ▼
TASK RUNS
  │
  ├─► [BL385] Default writes to session-local; explicit scope= to story/prd-shared
  ├─► [BL386] memory_handoff: write story-shared summary for next task
  └─► [BL386] memory_recall: full 6-layer hierarchy walk
  │
  ▼
TASK COMPLETE OR FAIL
  │
  ├─► [BL386 Phase 2] Harvest: session-local → story-shared / prd-shared
  │
  └─► [Phase 1a] VERIFIER FAIL path:
        verifier writes findings → prd-shared (role=verifier-finding)
        RetryHint: "N verifier findings in prd-shared (role: verifier-finding)"
        → retry task's warm-start includes verifier-finding memories
        → full finding detail available via memory_recall during retry
  │
  ▼
CHILD PRD SPAWNED  [Phase 1b]
  │
  └─► Seed parent.prd-shared → child.prd-shared at child PRD instantiation
        Breadcrumb: "seeded from parent prd/<parent_id>"
        Cap: 50 memories (max_per_scope)
  │
  ▼
PRD COMPLETE
  │
  └─► [Phase 3a] AUTO-REPORT (goroutine, non-blocking)
        Aggregate prd-shared + story-shared + session-local
        Store as PRD.memory_report (markdown) + memory_report_at
        Surface via autonomous_prd_get, PWA memory tile [Phase 3b]
  │
  ▼
[BL386] PRD DELETED → archive/purge/keep → project-shared
[BL386] NEW PRD → archive import from prior PRD archives
```

---

## Comm-channel surface

All BL387 commands use the `autonomous prd` prefix or the MCP tool name.

**Verifier memory (Phase 1a — automatic, no operator command)**
```
# View verifier findings stored in prd-shared:
memory scope recall project=<dir> prd_id=<id> query="verifier finding" scope=prd-shared
# MCP: memory_scope_recall project=<dir> prd_id=<id> scope=prd-shared query="verifier finding"
```

**Set memory seed (from_prds cross-seeding, Phase 2b)**
```
autonomous prd set-memory-seed prd_id=<id> enabled=true from_prds=<id1>,<id2> max_per_scope=20
# MCP: autonomous_prd_set_memory_seed prd_id=<id> enabled=true from_prds=[id1,id2] max_per_scope=20
# REST: PATCH /api/autonomous/prds/{id}/memory-seed  body: {from_prds: ["id1","id2"]}
```

**Set memory harvest (Phase 2 — BL386, also surfaced here)**
```
autonomous prd set-memory-harvest prd_id=<id> enabled=true promote_to=prd-shared role_filter=learning
# MCP: autonomous_prd_set_memory_harvest prd_id=<id> enabled=true promote_to=prd-shared
```

**Trigger PRD memory report (Phase 3a)**
```
autonomous prd memory-report prd_id=<id>
# MCP: autonomous_prd_memory_report prd_id=<id>
# REST: POST /api/autonomous/prds/{id}/memory-report
```

**Read auto-generated report from PRD record**
```
autonomous prd get prd_id=<id>   # → memory_report field in response
# MCP: autonomous_prd_get prd_id=<id>
# REST: GET /api/autonomous/prds/{id}  # → .memory_report, .memory_report_at
```

---

## Phase 1 — v8.31.0: Verifier memory + child PRD inheritance

### Feature 1a: Verifier memory

**What:** The BL103 verifier currently returns a `VerificationResult` containing
a free-text `RetryHint` string. When a task fails verification, the executor
passes `RetryHint` back to the retry session via `SpawnRequest.RetryHint`.

With BL385 `prd-shared` scope available, the verifier writes structured findings
directly to `prd-shared` memory — the retry task picks them up automatically via
BL386 warm-start seeding, so `RetryHint` carries a summary and the details live
in memory.

**Why:** `RetryHint` is truncated by context limits. Verifier findings can be
large (full diff analysis, test output). Writing to `prd-shared` lets the retry
task recall specific findings at any point during its run, not just at session
start.

**Changes:**

`internal/autonomous/verifier.go` (or equivalent BL103 verifier integration):

```go
// After verification, write findings to prd-shared scope
if !result.OK && s.memoryClient != nil {
    for _, finding := range result.Findings {
        s.memoryClient.ScopedSave(ScopedSaveReq{
            Scope:     "prd-shared",
            ProjectDir: task.ProjectDir,
            PRDID:     task.PRDID,
            Content:   fmt.Sprintf("[verifier-finding] %s\n\nFull: %s", finding.Summary, finding.Detail),
            Role:      "verifier-finding",
        })
    }
    // RetryHint still set for backward-compat; memory has the full detail
    result.RetryHint = fmt.Sprintf("%d verifier findings in prd-shared memory (role: verifier-finding)", len(result.Findings))
}
```

The retry task's warm-start seeding (BL386 Phase 1) includes `role_filter:
["verifier-finding"]` so it prioritizes these memories at session start.

**Phase 1a checklist:**

- [ ] Locate verifier integration in executor / BL103 path
- [ ] Add `ScopedSave` call after failed verification — writes to `prd-shared`
- [ ] `role: "verifier-finding"` for easy filtering
- [ ] `RetryHint` still populated (backward-compat) with summary + memory reference
- [ ] Retry task's `MemorySeedConfig.RoleFilter` includes `"verifier-finding"` when PRD has memory_seed enabled
- [ ] Unit: `TestBL387_Verifier_WritesFindings_ToPRDShared_OnFailure`
- [ ] Unit: `TestBL387_Verifier_NoWrite_OnSuccess`
- [ ] Unit: `TestBL387_Verifier_RetryHint_SummaryOnly_WhenMemoryEnabled`
- [ ] Integration: verifier failure → `GET /api/memory/scopes/borrow?scope=prd-shared&prd_id=X` returns findings
- [ ] Update `docs/testing-tracker.md`: verifier memory integration row
- [ ] Version bump: both version files → v8.31.0

---

### Feature 1b: Child PRD memory inheritance

**What:** When `SpawnPRD` creates a child PRD from a parent PRD task, the child
starts empty. It should inherit the parent's `prd-shared` memories by seeding
from `parent.prd-shared → child.prd-shared` at instantiation time.

**Why:** Child PRDs are refinements or sub-tasks of the parent. They need its
accumulated learnings (constraints found, patterns that worked, dead ends
discovered).

**Changes:**

`internal/autonomous/manager.go` — `recurseChildPRD` or child-PRD spawn path:

```go
// After child PRD is created and before its first task executes:
if m.memoryClient != nil && parent.MemorySeed.Enabled {
    m.memoryClient.ScopeSeed(ScopeSeedReq{
        From: ScopeRef{Scope: "prd-shared", Project: childPRD.ProjectDir, PRDID: parent.ID},
        To:   ScopeRef{Scope: "prd-shared", Project: childPRD.ProjectDir, PRDID: child.ID},
        Filter: SeedFilter{RolePrefix: ""},  // inherit all roles
        Limit: 50,
    })
}
```

The child PRD's `prd-shared` now contains the parent's accumulated knowledge
before its first task runs.

**Phase 1b checklist:**

- [ ] Locate `recurseChildPRD` / child PRD spawn in `internal/autonomous/`
- [ ] Add `ScopeSeed` call after child PRD ID is assigned and before executor starts
- [ ] Guard: only when parent has `MemorySeed.Enabled` AND memoryClient is non-nil
- [ ] Cap at 50 memories (configurable via `memory_seed.max_per_scope`)
- [ ] Breadcrumb in seeded memories: `"seeded from parent prd/<parent_id>"`
- [ ] Unit: `TestBL387_ChildPRD_InheritsParentPRDShared_WhenSeedEnabled`
- [ ] Unit: `TestBL387_ChildPRD_NoInheritance_WhenSeedDisabled`
- [ ] Unit: `TestBL387_ChildPRD_InheritanceCappedAt50`
- [ ] Integration: spawn child PRD → borrow from child prd-shared → parent memories present
- [ ] Update `docs/testing-tracker.md`: child PRD inheritance row

**Phase 1 release checklist:**

- [ ] All unit tests pass: `rtk go test ./internal/autonomous/... ./internal/mcp/...`
- [ ] All existing tests still pass: `rtk go test ./...` (2529+ tests)
- [ ] `docs/testing-tracker.md` rows added for both features (Tested=Yes)
- [ ] Both version files bumped to `v8.31.0`
- [ ] CHANGELOG v8.31.0 entry written
- [ ] README badge updated to v8.31.0
- [ ] E2e smoke: start PRD with memory_seed.enabled=true → fail one task → retry → verify retry session sees verifier-finding memories
- [ ] E2e smoke: spawn child PRD → verify child prd-shared contains parent memories
- [ ] `docs/testing-tracker.md` Validated column: mark Validated=Yes after live smoke passes
- [ ] **mobile-parity: datawatch-app#176 noted** — no Android/iOS mobile surface in Phase 1 (server-side only); parity issue filed for tracking
- [ ] Commit: `feat(autonomous): verifier memory + child PRD inheritance (BL387 Phase 1)`

---

## Phase 2 — v8.32.0: Decomposer enrichment + cross-PRD seeding

### Feature 2a: Decomposer context enrichment

**What:** Before the decomposer generates stories and tasks from a PRD spec, it
queries `project-shared` memory for relevant prior learnings and injects them as
context into the decomposer prompt.

**Why:** The decomposer currently operates only from the PRD spec text. It
re-discovers constraints, architectural decisions, and failure modes that may
already be in `project-shared` memory from prior PRDs. Injecting these prevents
wasted tasks and improves decomposition quality.

**Changes:**

`internal/autonomous/decomposer.go` (or wherever the decomposer prompt is built):

```go
// Before assembling the decomposer prompt, query project-shared for context
var priorContext string
if m.memoryClient != nil {
    hits, err := m.memoryClient.ScopedRecall(ScopedRecallReq{
        Project: prd.ProjectDir,
        // Walk project-shared + persona layers; no session/prd scope yet
        Layers:  []Scope{"project-shared", "persona-in-project"},
        Query:   prd.Spec,  // semantic search against the PRD spec text
        TopK:    15,
    })
    if err == nil && len(hits) > 0 {
        priorContext = formatPriorContext(hits)  // structured markdown block
    }
}

prompt := buildDecomposerPrompt(prd, priorContext)
```

The injected context appears as a fenced block in the decomposer system prompt:

```markdown
## Prior context from project memory

The following learnings from previous work in this project are relevant
to this PRD. Consider them when designing tasks — avoid repeating solved
problems and known dead-ends:

- [project-shared] Never use approach X — causes Y (from PRD abc123)
- [project-shared] Pattern Z is the preferred solution for W
```

**Phase 2a checklist:**

- [ ] Locate decomposer prompt builder in `internal/autonomous/`
- [ ] Add `ScopedRecall` call against `project-shared` before prompt assembly
- [ ] `formatPriorContext` helper: renders top-15 hits as fenced markdown block
- [ ] Guard: only when memoryClient non-nil AND at least 1 hit returned
- [ ] `max_context_memories` config field on PRD (default 15, max 30)
- [ ] Prompt injection does NOT modify PRD spec — context is additive only
- [ ] Unit: `TestBL387_Decomposer_InjectsProjectSharedContext_WhenMemoriesExist`
- [ ] Unit: `TestBL387_Decomposer_NoInjection_WhenNoMemoriesExist`
- [ ] Unit: `TestBL387_Decomposer_NoInjection_WhenMemoryClientNil`
- [ ] Unit: `TestBL387_FormatPriorContext_RendersCorrectly`
- [ ] Integration: write to project-shared → decompose PRD → verify context in assembled prompt
- [ ] Update `docs/testing-tracker.md`: decomposer enrichment row

---

### Feature 2b: Cross-PRD seeding

**What:** A PRD can declare `memory_seed.from_prds: [prd_id1, prd_id2]` in its
config. At first-task spawn time, the executor seeds the new PRD's `prd-shared`
from each listed PRD's `prd-shared` scope.

**Why:** Multi-PRD projects (a research PRD followed by an implementation PRD)
need to carry knowledge forward without the operator manually running
`memory_scope_seed` between PRDs.

**Changes:**

`PRD` struct:

```go
MemorySeed MemorySeedConfig  // existing (BL386)
```

`MemorySeedConfig` (BL386) extended:

```go
type MemorySeedConfig struct {
    Enabled      bool
    MaxPerScope  int
    RoleFilter   []string
    FromPRDs     []string  // NEW — cross-PRD seeding (BL387)
}
```

`autonomous_prd_set_memory_seed` MCP tool updated to accept `from_prds: [id, ...]`.

Executor at PRD first-task spawn:

```go
for _, fromPRDID := range prd.MemorySeed.FromPRDs {
    m.memoryClient.ScopeSeed(ScopeSeedReq{
        From: ScopeRef{Scope: "prd-shared", Project: prd.ProjectDir, PRDID: fromPRDID},
        To:   ScopeRef{Scope: "prd-shared", Project: prd.ProjectDir, PRDID: prd.ID},
        Limit: prd.MemorySeed.MaxPerScope,
        Filter: SeedFilter{RolePrefix: strings.Join(prd.MemorySeed.RoleFilter, ",")},
    })
}
```

**Phase 2b checklist:**

- [ ] Add `FromPRDs []string` to `MemorySeedConfig`
- [ ] Update `autonomous_prd_set_memory_seed` to accept `from_prds` param
- [ ] Executor seeds from each `FromPRDs` entry at PRD first-task spawn
- [ ] Guard: referenced PRDs must exist; log warning + skip if not found (non-fatal)
- [ ] MCP tool: `autonomous_prd_set_memory_seed` — REST, MCP, CLI, comm, PWA (5 surfaces)
- [ ] Unit: `TestBL387_CrossPRDSeed_SeedsFromListedPRDs_AtFirstTaskSpawn`
- [ ] Unit: `TestBL387_CrossPRDSeed_SkipsMissingPRD_WithWarning`
- [ ] Unit: `TestBL387_CrossPRDSeed_EmptyList_NoOp`
- [ ] Integration: PRD A with prd-shared memories → PRD B with from_prds=[A] → verify B's prd-shared populated

**Phase 2 release checklist:**

- [ ] All unit tests pass: `rtk go test ./internal/autonomous/...`
- [ ] Full suite: `rtk go test ./...`
- [ ] `docs/testing-tracker.md` rows added (Tested=Yes)
- [ ] Both version files → `v8.32.0`
- [ ] CHANGELOG v8.32.0 entry
- [ ] README badge → v8.32.0
- [ ] E2e smoke: write project-shared memory → decompose new PRD → verify memory context in decomposer session output
- [ ] E2e smoke: PRD A completes with prd-shared memories → PRD B with from_prds=[A] → first task of B has cross-PRD memories
- [ ] `docs/testing-tracker.md` Validated=Yes after live smokes
- [ ] **mobile-parity: datawatch-app#176** — Android PRD create/edit screen: `from_prds` multi-select (list active PRDs with prd-shared memories); locale keys below
- [ ] Locale keys (add to all 5 locale bundles + datawatch-app#176):
  - `memory_from_prds` — "Seed from prior PRDs"
  - `memory_from_prds_hint` — "Select PRDs whose memories should be inherited by this PRD at first task spawn"
- [ ] Commit: `feat(autonomous): decomposer enrichment + cross-PRD seeding (BL387 Phase 2)`

---

## Phase 3 — v8.33.0: Auto-report on completion + memory stats PWA card

### Feature 3a: Auto-report on PRD completion

**What:** When a PRD transitions to `completed` status, the executor automatically
calls `memory_prd_report` (BL386 Phase 4) and stores the result as an artifact on
the PRD record, fetchable via `autonomous_prd_get`.

**Why:** After a PRD finishes, the operator has no summary of what was learned. The
report must be manually requested. Auto-generating it on completion captures the
full scope picture while all session IDs are still known.

**Changes:**

`PRD` struct:

```go
MemoryReport string `json:"memory_report,omitempty"` // NEW — auto-generated markdown
MemoryReportAt *time.Time `json:"memory_report_at,omitempty"`
```

`internal/autonomous/manager.go` — `markPRDCompleted` (or completion transition):

```go
// After PRD transitions to completed
if m.memoryClient != nil {
    go func() {
        report, err := m.memoryClient.PRDReport(PRDReportReq{
            PRDID:         prd.ID,
            ProjectDir:    prd.ProjectDir,
            IncludeScopes: []string{"prd-shared", "story-shared"},
            MaxPerScope:   50,
            Format:        "markdown",
        })
        if err == nil {
            now := time.Now()
            m.store.SetPRDMemoryReport(prd.ID, report, &now)
        }
    }()
}
```

`autonomous_prd_get` response includes `memory_report` and `memory_report_at`.

New MCP tool `autonomous_prd_memory_report` for manual trigger / refresh.

**Phase 3a checklist:**

- [ ] Add `MemoryReport`, `MemoryReportAt` to `PRD` struct and store
- [ ] Add `SetPRDMemoryReport` to autonomous store
- [ ] Hook auto-report trigger into PRD completion transition
- [ ] Run in goroutine (non-blocking); log error if fails but don't fail the PRD
- [ ] `autonomous_prd_get` response includes report fields when non-empty
- [ ] New `autonomous_prd_memory_report` MCP tool (manual trigger)
- [ ] REST: `POST /api/autonomous/prds/{id}/memory-report` (manual trigger)
- [ ] Unit: `TestBL387_PRDCompletion_AutoTriggersMemoryReport`
- [ ] Unit: `TestBL387_PRDCompletion_MemoryReport_StoredOnPRD`
- [ ] Unit: `TestBL387_PRDMemoryReport_ManualTrigger_RefreshesReport`
- [ ] Integration: complete PRD → `autonomous_prd_get` → memory_report field populated
- [ ] Update `docs/testing-tracker.md`: auto-report row

---

### Feature 3b: Memory stats tile on PRD status card (PWA)

**What:** The PRD status view in the PWA (which already shows session resource bars
from BL380) gains a memory stats tile showing memories written per scope for this
PRD.

**Data source:** `GET /api/memory/scopes/inventory?project=<dir>` (BL386 Phase 5)
filtered to this PRD's `prd-shared` and related story/session scopes.

**Tile content:**
- `prd-shared`: N memories
- `story-shared`: N memories across M stories
- `session-local`: N memories across K task sessions
- Promoted: N (memories moved up from session-local)
- Last write: relative timestamp

**Changes:**

`internal/server/web/app.js` (or PWA component for PRD status):

```js
// In PRD status card render
async function renderPRDMemoryStats(prdID, projectDir) {
    const inv = await api('/api/memory/scopes/inventory?project=' + encodeURIComponent(projectDir));
    const prdScope  = inv['prd-shared']?.find(s => s.prd_id === prdID);
    const storyCount = (inv['story-shared'] || [])
        .filter(s => prd.stories.some(st => st.id === s.story_id))
        .reduce((n, s) => n + s.count, 0);
    // render tile
}
```

**Phase 3b checklist:**

- [ ] Requires BL386 Phase 5 scope inventory endpoint live
- [ ] PRD status card: new memory stats tile (count by scope)
- [ ] PWA: tile renders correctly when inventory returns 0 (graceful empty state)
- [ ] PWA: tile shows "—" when memoryClient not configured
- [ ] i18n: add locale keys for tile labels in en/de/es/fr/ja
- [ ] Unit (Go): no changes — frontend only
- [ ] Manual UI test: PRD with memory writes → status card shows correct counts
- [ ] Manual UI test: PRD with no memory → tile shows empty state (not broken)
- [ ] Screenshot evidence in `docs/testing-tracker.md`

**Phase 3 release checklist:**

- [ ] All unit tests pass: `rtk go test ./internal/autonomous/...`
- [ ] Full suite: `rtk go test ./...`
- [ ] `docs/testing-tracker.md` rows added (Tested=Yes for server-side; Validated=Yes after UI check)
- [ ] Both version files → `v8.33.0`
- [ ] CHANGELOG v8.33.0 entry
- [ ] README badge → v8.33.0
- [ ] E2e smoke: complete PRD → `autonomous_prd_get` returns `memory_report` field with markdown content
- [ ] E2e smoke: manual `autonomous_prd_memory_report` → returns report → same content as auto-generated
- [ ] E2e UI: open PWA PRD status card → memory tile visible → counts match scope inventory API
- [ ] `docs/testing-tracker.md` Validated=Yes for all Phase 3 features
- [ ] **mobile-parity: datawatch-app#176** — Android PRD detail screen: memory stats section (prd-shared count, story-shared count, session-local count, last write timestamp); locale keys below
- [ ] Locale keys (add to all 5 locale bundles + datawatch-app#176):
  - `prd_memory_stats` — "Memory Stats"
  - `memory_scope_prd_shared` — "PRD-shared memories"
  - `memory_scope_story_shared` — "Story-shared memories"
  - `memory_scope_session_local` — "Session-local memories"
- [ ] Commit: `feat(autonomous): auto PRD memory report + memory stats PWA card (BL387 Phase 3)`

---

## End-to-end test scenarios (BL387)

These must be written as integration test functions (not manual-only). Tests live in
`internal/autonomous/bl387_e2e_test.go`.

### Scenario E1: Full verifier-feedback loop

```
1. Create PRD with memory_seed.enabled=true, role_filter=["verifier-finding"]
2. Run task → verifier fails with 3 findings
3. Assert: prd-shared contains 3 memories with role=verifier-finding
4. Assert: RetryHint references memory count
5. Retry task spawns → assert session seeded with verifier-finding memories
6. Retry task passes → assert no new verifier-finding memories added
```

### Scenario E2: Child PRD knowledge inheritance

```
1. Create parent PRD with memory_seed.enabled=true
2. Complete 2 parent tasks → write 5 memories to prd-shared
3. Spawn child PRD from parent task
4. Assert: child prd-shared contains parent's memories (with breadcrumb)
5. Run child task → assert it finds parent memories via memory_recall
```

### Scenario E3: Decomposer uses prior context

```
1. Write 3 project-shared memories (role=pattern, learning, decision)
2. Create new PRD with same project_dir
3. Trigger decomposition
4. Assert: decomposer session prompt includes prior-context block
5. Assert: generated tasks do not duplicate solved problems in the memories
```

### Scenario E4: Cross-PRD seeding chain

```
1. PRD A completes — 10 prd-shared memories accumulated
2. PRD B configured with from_prds=[A.id]
3. First task of PRD B spawns
4. Assert: PRD B prd-shared contains PRD A's memories
5. Assert: PRD B's task session (via warm-start) sees cross-PRD memories
```

### Scenario E5: Auto-report on completion

```
1. PRD with 2 stories, 3 tasks each completes
2. Assert: PRD record has memory_report (non-empty markdown) within 5s of completion
3. Assert: report references prd-shared count and story-shared count
4. Call autonomous_prd_memory_report manually → assert returns same content
```

**E2e test checklist:**

- [ ] `internal/autonomous/bl387_e2e_test.go` created with all 5 scenarios
- [ ] All 5 scenarios pass with in-process test doubles (no real Claude calls)
- [ ] Validated: at least Scenario E1 and E5 run against a live daemon with `make smoke`
- [ ] `docs/testing-tracker.md`: BL387 e2e section added, Tested=Yes for all 5
- [ ] `docs/testing-tracker.md`: Validated=Yes for E1, E5 after live run
- [ ] Comm-channel verification: `autonomous prd set-memory-seed prd_id=<id> from_prds=...` works via MCP + REST + comm channel
- [ ] Comm-channel verification: `autonomous prd memory-report prd_id=<id>` returns markdown report via all 3 surfaces
- [ ] Comm-channel verification: `memory scope recall scope=prd-shared prd_id=<id> query="verifier"` returns verifier-finding memories

---

## Out of scope

- Memory tagging / labels beyond role field — BL388 (future)
- Auto-promote policy engine — BL388 (future)
- Memory quota / per-scope limits — BL388 (future)
- Decomposer using prd-shared from same PRD (first decompose has no prd memory yet — by definition)
- verifier writing to session-local (not useful — retry tasks don't share session)
