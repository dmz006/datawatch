# BL385 — Subprocess Memory Scope Isolation + PRD/Story/Project Shared Memory

**Status:** Draft  
**Target release:** v8.29.0 (minor — new scope layers + new endpoint + behavior change)  
**Filed:** 2026-09-15  
**Author:** dmz006

---

## Problem

After v8.28.7, all 13 memory tools in subprocess MCP mode proxy to the flat global
memory store via HTTP loopback. This means any tool (opencode, Goose, or any future
client) that spawns `datawatch mcp` as a stdio subprocess can read and write the
operator's entire memory store without any scoping.

Specific risks:

- **Unintended global pollution**: An opencode session working on project A can
  write memories visible to all future sessions across all projects.
- **Noisy recall**: `memory_recall` in a subprocess returns global results, not
  results relevant to the subprocess's project and task context.
- **No shared working memory**: Tasks within the same PRD story cannot share
  learnings. A task that discovers a useful pattern has no way to broadcast it to
  sibling tasks within the same PRD or story without writing to global memory.
- **Bulk mutation hazard**: `memory_sweep_stale` or `memory_import` from a subprocess
  affects the entire global store.

The 4-layer scope hierarchy (`ScopePersonaGlobal → ScopePersonaInProject →
ScopeProjectShared → ScopeSessionLocal`) and all scope REST/MCP surface already
exist (v7.0.0 BL295). The missing pieces are:

1. Two new scope layers: `prd-shared` and `story-shared`
2. Subprocess mode defaulting to `session-local` writes and scope-hierarchy reads
3. Executor injecting PRD/story context into spawned subprocess sessions

---

## Workflow diagram

```
NON-SUBPROCESS SESSION (claude-code interactive, operator CLI, PWA)
  │
  └─► Flat global memory store — memory_remember writes globally
        memory_recall returns global results
        memory_sweep_stale sweeps the entire store

SUBPROCESS SESSION (opencode, Goose, executor task — spawned via "datawatch mcp")
  │
  │  Daemon injects context flags at spawn:
  │    --caller-session-id <session_id>
  │    --caller-prd-id     <prd_id>      (NEW BL385)
  │    --caller-story-id   <story_id>    (NEW BL385)
  │
  └─► 6-layer scope hierarchy (read: top→bottom; write default: session-local)

       [1] persona-global      (projectDir="",       role="persona/<n>", sessionID="")
       [2] persona-in-project  (projectDir,          role="persona/<n>", sessionID="")
       [3] project-shared      (projectDir,          role="",            sessionID="")
       [4] prd-shared     NEW  (projectDir,          role="prd/<id>",    sessionID="")
       [5] story-shared   NEW  (projectDir,          role="story/<id>",  sessionID="")
       [6] session-local       (projectDir,          role="",            sessionID=callerSessionID)
              ▲
              └─ memory_remember (no scope=) writes HERE in subprocess mode

       Recall walks [6]→[5]→[4]→[3]→[2]→[1], merges results, labels by layer.

       Blocked in subprocess mode: memory_sweep_stale, memory_import
       Informed (use promote first): memory_pin

PROMOTION / SHARING (operator or auto-harvest BL386)
  session-local  ──promote──►  story-shared  ──promote──►  prd-shared  ──promote──►  project-shared
```

---

## Scope hierarchy (extended)

The existing 4-layer model is extended to 6 layers, slotting between `project-shared`
and `session-local`:

```
persona-global      (persona, ALL projects)
persona-in-project  (persona, specific project)
project-shared      (all sessions across ALL PRDs in this project)   ← existing
prd-shared          (all stories/tasks within ONE PRD)               ← NEW
story-shared        (all tasks within ONE story)                      ← NEW
session-local       (single task session)
```

Recall walks top-down (most-general first). Writes default to `session-local`.
Promotion moves memories up the hierarchy with breadcrumb provenance.

---

## Storage mapping (backend-transparent)

The scope layers project onto the existing `(projectDir, role, sessionID)` Backend
tuple — no Backend interface changes:

| Scope | projectDir | role | sessionID |
|-------|-----------|------|-----------|
| `persona-global` | `""` | `"persona/<name>"` | `""` |
| `persona-in-project` | `<project>` | `"persona/<name>"` | `""` |
| `project-shared` | `<project>` | `""` | `""` |
| `prd-shared` | `<project>` | `"prd/<prd_id>"` | `""` | ← NEW |
| `story-shared` | `<project>` | `"story/<story_id>"` | `""` | ← NEW |
| `session-local` | `<project>` | `""` | `<session_id>` |
| `discussion` | `""` | `"discussion/<id>"` | `""` |

This follows the same pattern as `persona-in-project` (role-namespaced within
project). No schema changes, no migration.

---

## Context injection into subprocess sessions

The autonomous executor already knows the PRD, story, and task IDs for each spawned
session. Two new CLI flags parallel the existing `--caller-session-id`:

```
datawatch mcp \
  --caller-session-id <session_full_id> \
  --caller-prd-id     <prd_id>          \   ← NEW
  --caller-story-id   <story_id>            ← NEW
```

The executor injects these in the same place it currently injects `--caller-session-id`
(session manager, `hookEnv` or equivalent MCP spawn path).

Non-PRD subprocess sessions (opencode, Goose manual use) omit `--caller-prd-id` and
`--caller-story-id`. The MCP server treats empty values as "no PRD/story scope
available" and falls back to `session-local` only.

---

## Changes to `internal/memory/scopes.go`

### New Scope constants

```go
const (
    ScopePersonaGlobal    Scope = "persona-global"
    ScopePersonaInProject Scope = "persona-in-project"
    ScopeProjectShared    Scope = "project-shared"
    ScopePRDShared        Scope = "prd-shared"    // NEW — BL385
    ScopeStoryShared      Scope = "story-shared"  // NEW — BL385
    ScopeSessionLocal     Scope = "session-local"
    ScopeDiscussion       Scope = "discussion"
)
```

### Extended ScopeRef

```go
type ScopeRef struct {
    Scope     Scope
    Persona   string
    Project   string
    SessionID string
    PRDID     string  // NEW — for prd-shared scope
    StoryID   string  // NEW — for story-shared scope
}
```

### Extended Resolve()

```go
case ScopePRDShared:
    return sr.Project, "prd/" + sr.PRDID, ""
case ScopeStoryShared:
    return sr.Project, "story/" + sr.StoryID, ""
```

### Extended AllScopesTopDown

```go
var AllScopesTopDown = []Scope{
    ScopePersonaGlobal,
    ScopePersonaInProject,
    ScopeProjectShared,
    ScopePRDShared,    // NEW
    ScopeStoryShared,  // NEW
    ScopeSessionLocal,
    ScopeDiscussion,
}
```

---

## Changes to `internal/mcp/server.go`

### New fields

```go
callerSessionID string  // existing
callerPRDID     string  // NEW — set by --caller-prd-id
callerStoryID   string  // NEW — set by --caller-story-id
```

### New setters (parallel to SetCallerSessionID)

```go
func (s *Server) SetCallerPRDID(id string)   { s.callerPRDID = id }
func (s *Server) SetCallerStoryID(id string) { s.callerStoryID = id }
```

### subprocessMode helper

```go
func (s *Server) subprocessMode() bool {
    return s.memoryAPI == nil && s.webPort > 0 && s.callerSessionID != ""
}
```

---

## Changes to `cmd/datawatch/main.go`

Two new flags on the `mcp` subcommand (parallel to `--caller-session-id`):

```go
cmd.Flags().String("caller-prd-id",   "", "PRD ID of the PRD this MCP session was spawned for")
cmd.Flags().String("caller-story-id", "", "Story ID of the story this MCP session was spawned for")
```

Wire to the new setters after `SetCallerSessionID`.

---

## New REST endpoint: `POST /api/memory/scopes/save`

Scope-aware write. Replaces the need to use `seed` (copy) just to do a targeted
write to a specific scope.

```
POST /api/memory/scopes/save
Authorization: Bearer <token>

{
  "scope":      "session-local",        // required; one of the 6 scope names
  "project":    "/home/user/datawatch", // required (except persona-global)
  "session_id": "johnnyjohnny-12cf",    // required for session-local
  "prd_id":     "0fb4e302",             // required for prd-shared
  "story_id":   "9b90b7a5",             // required for story-shared
  "persona":    "",                     // required for persona-* scopes
  "content":    "the memory text",
  "role":       ""                      // optional; applied on top of scope role
}

→ 200 OK
{
  "memory_id": 42,
  "scope":      "session-local",
  "resolved":   {"project_dir": "...", "role": "", "session_id": "12cf"}
}
```

---

## Changes to `internal/mcp/memory_tools.go`

### memory_remember — scope routing

New optional `scope` parameter (string enum) and `global` bool:

```go
mcpsdk.WithString("scope",
    mcpsdk.Description(
        "Target scope for this memory. Default in subprocess: session-local. "+
        "Options: session-local | story-shared | prd-shared | project-shared | global. "+
        "global writes to the flat global store (bypasses all scope isolation).",
    ),
)
mcpsdk.WithBool("global",
    mcpsdk.Description("Shorthand for scope=global. Deprecated; prefer scope= param."),
)
```

Routing logic in subprocess mode:

```go
if s.subprocessMode() {
    scope := optString(req, "scope")
    isGlobal, _ := req.Params.Arguments["global"].(bool)
    if isGlobal { scope = "global" }
    if scope == "" { scope = "session-local" }  // default

    if scope != "global" {
        body := s.buildScopedSaveBody(scope, projectDir, content)
        if r, ok := s.proxyMemoryPOST("/api/memory/scopes/save", body); ok {
            return r, nil
        }
    }
}
// global or non-subprocess: existing path
```

`buildScopedSaveBody` constructs the JSON body using `callerSessionID`,
`callerPRDID`, `callerStoryID` from the server struct based on the scope:

```go
func (s *Server) buildScopedSaveBody(scope, projectDir, content string) map[string]any {
    return map[string]any{
        "scope":      scope,
        "project":    projectDir,
        "session_id": s.callerSessionID,
        "prd_id":     s.callerPRDID,
        "story_id":   s.callerStoryID,
        "content":    content,
    }
}
```

### memory_recall — scoped walk

In subprocess mode, proxy to `/api/memory/scopes/recall` with full context:

```
GET /api/memory/scopes/recall?project=...&session=...&prd_id=...&story_id=...&top_k=N
```

This walks: `persona-global → persona-in-project → project-shared → prd-shared →
story-shared → session-local` and returns merged results with layer attribution.

The `ScopedRecall` function already handles variable layers — we pass the new
`PRDID`/`StoryID` through `ScopeRef` and the scope is skipped if the ID is empty
(same as how `session-local` is skipped when `sessionID` is empty today).

### memory_list — scoped listing

In subprocess mode, list in reverse specificity order (most specific first):

1. `session-local` list
2. `story-shared` list (if `callerStoryID != ""`)
3. `prd-shared` list (if `callerPRDID != ""`)

Return merged, labeled by scope.

### Blocked in subprocess mode

`memory_sweep_stale` and `memory_import` return a clear error:

```
memory_sweep_stale is blocked in subprocess mode to prevent global data loss.
Use memory_scope_promote to surface session memories, then sweep from the operator CLI.
```

### memory_pin in subprocess mode

```
Pinning is only available for global memories. Use memory_scope_promote to move
this memory to project-shared first, then pin from an operator session.
```

---

## Comm-channel surface

All new scope operations are reachable from comm channels (Signal, Telegram, Slack,
etc.) using the existing `memory` command family:

```
# Write to a specific scope
memory scope save  scope=prd-shared  project=/home/user/proj  prd_id=0fb4e302  text="found that X approach causes OOM"

# Write to story scope
memory scope save  scope=story-shared  project=/home/user/proj  story_id=9b90b7a5  text="pattern Z works here"  role=learning

# Delete an entire scope (e.g. cleanup after PRD deleted)
memory scope delete  scope=prd-shared  project=/home/user/proj  prd_id=0fb4e302

# Recall across hierarchy for a session in a PRD context
memory scope recall  project=/home/user/proj  session_id=johnnyjohnny-12cf  prd_id=0fb4e302  story_id=9b90b7a5  query="approach for X"

# List all scopes with data
memory scope inventory  project=/home/user/proj
```

These comm commands proxy to the same REST endpoints as the MCP tools.

---

## Executor changes: inject PRD/story context

In the autonomous executor's session-spawn path, when launching a task session via
`datawatch mcp`, inject `--caller-prd-id` and `--caller-story-id` alongside the
existing `--caller-session-id`. The values come directly from the `Task` struct
(`task.PRDID`, `task.StoryID`).

Location: same place `--caller-session-id` is injected today (session manager
MCP command builder or `hookEnv`). One new `fmt.Sprintf` per flag.

---

## Tool behavior summary (post-BL385)

| Tool | Non-subprocess | Subprocess (no prd/story) | Subprocess (with prd+story) |
|------|---------------|--------------------------|----------------------------|
| `memory_remember` | global | `session-local` (default) | `session-local` (default); `story-shared`/`prd-shared`/`project-shared` via `scope=` param |
| `memory_recall` | global flat | hierarchy walk (session+project) | hierarchy walk (session+story+prd+project) |
| `memory_list` | global | session-local | session+story+prd lists, merged |
| `memory_forget` | global | global (complex to scope — deferred) | global |
| `memory_pin` | global | inform: use promote first | inform: use promote first |
| `memory_sweep_stale` | global | blocked | blocked |
| `memory_import` | global | blocked | blocked |
| `memory_stats` | global | global | global |
| `memory_schema_version` | global | global | global |
| `memory_export` | global | global | global |
| `memory_spellcheck` | global | global (stateless) | global (stateless) |
| `memory_extract_facts` | global | global (stateless) | global (stateless) |
| `memory_learnings` | global | hierarchy walk | hierarchy walk (story+prd layers) |

---

## Promotion flow (unchanged API, new use case)

After a task session completes, the operator (or automated post-task hook) can
surface useful memories from the session up to story or PRD scope:

```
# Promote one session-local memory to story-shared
memory_scope_promote
  memory_id=42
  from_scope=session-local from_session=12cf from_project=/home/user/datawatch
  to_scope=story-shared    to_project=/home/user/datawatch
  → stores as (project, "story/9b90b7a5", "")

# Promote to PRD-shared (visible to all stories in the PRD)
memory_scope_promote
  memory_id=42
  from_scope=session-local from_session=12cf from_project=/home/user/datawatch
  to_scope=prd-shared      to_project=/home/user/datawatch
  → stores as (project, "prd/0fb4e302", "")

# Bulk seed all story-local memories from one task into story-shared
memory_scope_seed
  from_scope=session-local from_session=12cf from_project=/home/user/datawatch
  to_scope=story-shared    to_project=/home/user/datawatch
  [story_id auto-derived from the from_session if executor injects it]
```

No new promotion tools needed — `memory_scope_promote` and `memory_scope_seed`
already support arbitrary `ScopeRef` source/target pairs. The `to_scope` field
just needs to accept `"prd-shared"` and `"story-shared"` as valid values, which
falls out of the new Scope constants.

---

## Files to change

| File | Change |
|------|--------|
| `internal/memory/scopes.go` | `ScopePRDShared`, `ScopeStoryShared` constants; `PRDID`/`StoryID` on `ScopeRef`; extend `Resolve()` + `AllScopesTopDown` |
| `internal/mcp/server.go` | `callerPRDID`, `callerStoryID` fields; `SetCallerPRDID`, `SetCallerStoryID`; `subprocessMode()` |
| `internal/mcp/memory_tools.go` | `memory_remember` scope routing; `memory_recall`/`memory_list` hierarchy walk; block sweep/import; inform pin |
| `internal/server/memory_scopes.go` | `memoryScopeSave` handler; extend `handleMemoryScopes` switch; update `memoryRecall` to pass prd_id/story_id query params |
| `internal/server/server.go` | No route changes — `/api/memory/scopes/` already catches `/save` |
| `cmd/datawatch/main.go` | `--caller-prd-id`, `--caller-story-id` flags; wire to setters; version → v8.29.0 |
| `internal/server/api.go` | Version → v8.29.0 |
| `internal/autonomous/executor.go` (or session manager) | Inject `--caller-prd-id`, `--caller-story-id` at MCP spawn |
| `CHANGELOG.md` | v8.29.0 entry |
| `README.md` | Badge + release entry |
| `docs/implementation.md` | New `/api/memory/scopes/save` endpoint |

---

## Phase breakdown

### Phase 1 — Scope model extension

1. Add `ScopePRDShared`, `ScopeStoryShared` to `internal/memory/scopes.go`.
2. Add `PRDID`, `StoryID` to `ScopeRef`.
3. Extend `Resolve()` for new scopes.
4. Extend `AllScopesTopDown`.
5. Verify `ScopedRecall` skips prd/story layers when PRDID/StoryID is empty
   (same guard logic as `ScopeSessionLocal` skip when sessionID is empty).
6. Tests:
   - `TestBL385_ScopeRef_Resolve_PRDShared`
   - `TestBL385_ScopeRef_Resolve_StoryShared`
   - `TestBL385_ScopedRecall_SkipsPRDLayer_WhenPRDIDEmpty`
   - `TestBL385_ScopedRecall_SkipsStoryLayer_WhenStoryIDEmpty`
   - `TestBL385_ScopedRecall_IncludesPRDAndStoryLayers_WhenIDsProvided`

**Phase 1 release checklist:**

- [ ] All 5 scope model tests pass: `rtk go test ./internal/memory/...`
- [ ] Full suite unchanged: `rtk go test ./...`
- [ ] `docs/testing-tracker.md`: Phase 1 scope model row added (Tested=Yes)
- [ ] E2e smoke: write to prd-shared scope via REST → recall with prd_id → memory returned
- [ ] Validated=Yes in testing-tracker.md after live smoke

### Phase 2 — REST endpoint

1. Add `memoryScopeDelete` in `internal/server/memory_scopes.go`.
   - `POST /api/memory/scopes/delete`
   - Body: `{scope, project, session_id, prd_id, story_id, persona, dry_run}`
   - Resolves `ScopeRef` → `(projectDir, role, sessionID)` → bulk-deletes matching rows
   - Returns `{deleted, scope, resolved, dry_run}`
   - Required by BL386 archive-on-delete strategy
   - Tests: `TestBL385_ScopeDelete_SessionLocal`, `TestBL385_ScopeDelete_PRDShared`,
     `TestBL385_ScopeDelete_DryRun_ReturnsCountWithoutDeleting`

2. Add `memoryScopeSave` in `internal/server/memory_scopes.go`.
   - Decode `{scope, project, session_id, prd_id, story_id, persona, content, role}`.
   - Construct `ScopeRef` and call `Resolve()` to get `(projectDir, role, sessionID)`.
   - Write via existing `backend.Save`.
   - Return `{memory_id, scope, resolved}`.
2. Extend `handleMemoryScopes` switch: `rest == "save"`.
3. Update `memoryRecall` to forward `prd_id` and `story_id` query params through
   to `ScopedRecall` via the extended `ScopeRef`.
4. Tests:
   - `TestBL385_ScopeSave_SessionLocal_WritesCorrectKey`
   - `TestBL385_ScopeSave_PRDShared_WritesCorrectKey`
   - `TestBL385_ScopeSave_StoryShared_WritesCorrectKey`
   - `TestBL385_ScopeSave_ProjectShared_WritesCorrectKey`
   - `TestBL385_ScopeSave_MissingScope_Returns400`
   - `TestBL385_ScopeSave_PRDShared_MissingPRDID_Returns400`
   - `TestBL385_ScopeSave_StoryShared_MissingStoryID_Returns400`

**Phase 2 release checklist:**

- [ ] All 10 REST tests pass: `rtk go test ./internal/server/...`
- [ ] Full suite: `rtk go test ./...`
- [ ] `docs/testing-tracker.md`: Phase 2 REST endpoint row (Tested=Yes)
- [ ] fedCap guard added to `POST /api/memory/scopes/save` and `POST /api/memory/scopes/delete`
- [ ] E2e smoke: `POST /api/memory/scopes/save` → verify stored; `POST /api/memory/scopes/delete` → verify purged
- [ ] Validated=Yes after live smoke

### Phase 3 — MCP server subprocess routing

1. Add `callerPRDID`, `callerStoryID` to `mcp.Server`; add setters.
2. Add `subprocessMode()`.
3. Add `--caller-prd-id`, `--caller-story-id` flags in `cmd/datawatch/main.go`.
4. Update `handleMemoryRemember`: `scope` param + `buildScopedSaveBody`.
5. Update `handleMemoryRecall`: scope-hierarchy walk in subprocess mode.
6. Update `handleMemoryList`: session+story+prd list in subprocess mode.
7. Block `handleMemorySweep` + `handleMemoryImport` in subprocess mode.
8. Inform `handleMemoryPin` in subprocess mode.
9. Tests:
   - `TestBL385_SubprocessMode_Remember_DefaultsTo_SessionLocal`
   - `TestBL385_SubprocessMode_Remember_ScopeStoryShared_Routes_Correctly`
   - `TestBL385_SubprocessMode_Remember_ScopePRDShared_Routes_Correctly`
   - `TestBL385_SubprocessMode_Remember_ScopeProjectShared_Routes_Correctly`
   - `TestBL385_SubprocessMode_Remember_GlobalTrue_Routes_ToGlobal`
   - `TestBL385_SubprocessMode_Recall_WalksHierarchy_WithPRDAndStory`
   - `TestBL385_SubprocessMode_Recall_WalksHierarchy_NoPRDOrStory`
   - `TestBL385_SubprocessMode_List_MergesSessionStoryPRD`
   - `TestBL385_SubprocessMode_Sweep_Returns_BlockedError`
   - `TestBL385_SubprocessMode_Import_Returns_BlockedError`
   - `TestBL385_SubprocessMode_Pin_Returns_Inform`
   - `TestBL385_NonSubprocessMode_Remember_RoutesTo_GlobalSave`

**Phase 3 release checklist:**

- [ ] All 12 MCP subprocess routing tests pass: `rtk go test ./internal/mcp/...`
- [ ] Full suite: `rtk go test ./...`
- [ ] `docs/testing-tracker.md`: Phase 3 MCP routing row (Tested=Yes)
- [ ] E2e smoke: spawn `datawatch mcp --caller-session-id X --caller-prd-id Y --caller-story-id Z`; call `memory_remember`; verify written to session-local not global
- [ ] E2e smoke: call `memory_sweep_stale` in subprocess → verify blocked error returned
- [ ] Validated=Yes after live smoke

### Phase 4 — Executor injection

1. Locate the MCP spawn path in the autonomous executor or session manager
   (where `--caller-session-id` is currently injected).
2. Inject `--caller-prd-id <task.PRDID>` and `--caller-story-id <task.StoryID>`
   alongside it when both are non-empty.
3. Tests:
   - `TestBL385_Executor_InjectsPRDAndStoryID_WhenSpawningMCPSession`
   - `TestBL385_Executor_OmitsPRDAndStoryID_WhenNotPRDTask`

**Phase 4 release checklist:**

- [ ] Both executor injection tests pass
- [ ] Full suite: `rtk go test ./...`
- [ ] `docs/testing-tracker.md`: Phase 4 executor injection row (Tested=Yes)
- [ ] E2e smoke: run PRD task → verify subprocess session receives correct prd_id/story_id via `memory_recall` scope attribution

### Phase 5 — Docs + version bump

1. Bump both version files to `v8.29.0`.
2. CHANGELOG entry.
3. README badge.
4. `docs/implementation.md` — new `/api/memory/scopes/save` endpoint.
5. `docs/howto/prd-memory-workflow.md` — new how-to for PRD memory workflow.
6. `docs/datawatch-definitions.md` — add `prd-shared`, `story-shared`, `subprocess mode` definitions.
7. `docs/parity-status.md` — add memory-scope row (PWA | Android | iOS).

**Phase 5 release checklist (v8.29.0 full release):**

- [ ] `rtk go test ./...` — all tests pass, zero failures
- [ ] ZAP scan: no new WARN/FAIL on new endpoints
- [ ] `scripts/release-smoke.sh` passes (new scope save/delete/recall smoke sections added)
- [ ] Both version files → `v8.29.0`
- [ ] CHANGELOG v8.29.0 entry written
- [ ] README badge → v8.29.0
- [ ] `docs/implementation.md` updated with new endpoints
- [ ] `docs/howto/prd-memory-workflow.md` written
- [ ] `docs/datawatch-definitions.md` updated
- [ ] `docs/parity-status.md` memory-scope row added
- [ ] mobile-parity: datawatch-app#174 filed ✓ (already filed)
- [ ] fedCap guard verified on `POST /api/memory/scopes/save` and `POST /api/memory/scopes/delete`
- [ ] All 5 phases: Validated=Yes in testing-tracker.md
- [ ] Tag: `git tag v8.29.0` → CI handles goreleaser + containers
- [ ] **Do NOT** run `make cross` or `gh release create` manually

---

## Out of scope

- `memory_forget` scope targeting — needs memory-ID-to-scope reverse lookup. Left
  global for now; follow-up item.
- PWA promotion UI — operator uses MCP/CLI; dedicated UI is a separate future item.
- Auto-promote on task/story completion — that is BL386 (harvest policy).
- Warm-start seeding at session spawn — that is BL386 (auto-seed at spawn).
- Archive-on-delete (promote before purge) — that is BL386.
- Memory handoff, PRD report, scope inventory, scope TTL — all BL386.
- Changing the Backend interface — non-breaking by design (role-namespacing convention).
- Encryption or access control on scopes — orthogonal to BL68/BL70.

---

## E2e test scenarios (`internal/mcp/bl385_e2e_test.go`)

In-process daemon with real SQLite memory backend + fake LLM. No external services.

**E1: Subprocess write isolation**
```
1. Start daemon; write 5 global memories
2. Spawn subprocess MCP session with --caller-session-id=X --caller-prd-id=Y --caller-story-id=Z
3. Call memory_remember (no scope=) → assert writes to session-local, NOT global
4. Global count still = 5; session-local count for session X = 1
```

**E2: Subprocess recall hierarchy**
```
1. Write 1 memory to project-shared, prd-shared (prd Y), story-shared (story Z), session-local (session X)
2. In subprocess session X/Y/Z: call memory_recall query="test"
3. Assert: all 4 memories returned, labeled by scope layer
4. Assert: recall order is session-local first (most specific)
```

**E3: Scoped delete removes only target scope**
```
1. Write 3 memories to prd-shared (prd Y), 2 to project-shared
2. POST /api/memory/scopes/delete {scope: "prd-shared", prd_id: Y}
3. Assert: prd-shared count = 0
4. Assert: project-shared count still = 2
```

**E4: Scope inventory returns correct counts**
```
1. Write memories across 3 scopes: prd-shared (prd Y), story-shared (story Z), project-shared
2. GET /api/memory/scopes/inventory?project=...
3. Assert: response contains all 3 scope types with correct counts
```

**E5: Non-subprocess session — flat global behavior unchanged**
```
1. Interactive session (memoryAPI set, webPort 0)
2. Call memory_remember → assert writes to global flat store
3. Call memory_recall → assert returns global flat results
4. Call memory_sweep_stale → assert NOT blocked (works normally)
5. Assert: no regression from pre-BL385 behavior
```

**E2e checklist:**

- [ ] `internal/mcp/bl385_e2e_test.go` created with all 5 scenarios
- [ ] All 5 pass with in-process test doubles
- [ ] Comm-channel commands E3/E4 verified via `POST /api/test/message` (e.g. `memory scope delete ...`)
- [ ] `docs/testing-tracker.md`: BL385 e2e section added (Tested=Yes for all 5)
- [ ] Validated=Yes for E1 and E5 after live daemon run

---

## Relationship to BL386

BL385 is the mechanics foundation. BL386 (`docs/plans/2026-09-15-bl386-memory-lifecycle-management.md`)
builds lifecycle policies on top: warm-start seeding, harvest on completion,
archive-on-delete, handoff tool, PRD memory report, scope inventory, scope-aware TTL.
BL386 cannot ship without BL385.

---

## Test names (full list)

```
# Phase 1 — scope model
TestBL385_ScopeRef_Resolve_PRDShared
TestBL385_ScopeRef_Resolve_StoryShared
TestBL385_ScopedRecall_SkipsPRDLayer_WhenPRDIDEmpty
TestBL385_ScopedRecall_SkipsStoryLayer_WhenStoryIDEmpty
TestBL385_ScopedRecall_IncludesPRDAndStoryLayers_WhenIDsProvided

# Phase 2 — REST endpoint
TestBL385_ScopeSave_SessionLocal_WritesCorrectKey
TestBL385_ScopeSave_PRDShared_WritesCorrectKey
TestBL385_ScopeSave_StoryShared_WritesCorrectKey
TestBL385_ScopeSave_ProjectShared_WritesCorrectKey
TestBL385_ScopeSave_MissingScope_Returns400
TestBL385_ScopeSave_PRDShared_MissingPRDID_Returns400
TestBL385_ScopeSave_StoryShared_MissingStoryID_Returns400

# Phase 3 — MCP subprocess routing
TestBL385_SubprocessMode_Remember_DefaultsTo_SessionLocal
TestBL385_SubprocessMode_Remember_ScopeStoryShared_Routes_Correctly
TestBL385_SubprocessMode_Remember_ScopePRDShared_Routes_Correctly
TestBL385_SubprocessMode_Remember_ScopeProjectShared_Routes_Correctly
TestBL385_SubprocessMode_Remember_GlobalTrue_Routes_ToGlobal
TestBL385_SubprocessMode_Recall_WalksHierarchy_WithPRDAndStory
TestBL385_SubprocessMode_Recall_WalksHierarchy_NoPRDOrStory
TestBL385_SubprocessMode_List_MergesSessionStoryPRD
TestBL385_SubprocessMode_Sweep_Returns_BlockedError
TestBL385_SubprocessMode_Import_Returns_BlockedError
TestBL385_SubprocessMode_Pin_Returns_Inform
TestBL385_NonSubprocessMode_Remember_RoutesTo_GlobalSave

# Phase 4 — executor injection
TestBL385_Executor_InjectsPRDAndStoryID_WhenSpawningMCPSession
TestBL385_Executor_OmitsPRDAndStoryID_WhenNotPRDTask
```
