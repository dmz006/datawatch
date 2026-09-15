# BL385 — Subprocess Memory Scope Isolation

**Status:** Draft  
**Target release:** v8.29.0 (minor — new endpoint + behavior change)  
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
  accidentally write memories visible to all future sessions, all projects.
- **Noisy recall**: `memory_recall` in a subprocess returns global results, not
  results relevant to the subprocess's project and task context.
- **Bulk mutation hazard**: `memory_sweep_stale` called from a subprocess could
  affect memories from other projects and sessions.

The 4-layer scope hierarchy (`ScopePersonaGlobal → ScopePersonaInProject →
ScopeProjectShared → ScopeSessionLocal`) and all the scope REST/MCP surface already
exist (v7.0.0 BL295). The missing piece is wiring subprocess mode to default to
`session-local` writes and scope-aware reads.

---

## Goal

When `datawatch mcp` is in **subprocess mode** (`memoryAPI == nil`, `webPort > 0`,
`callerSessionID != ""`):

| Tool | Current (v8.28.7) | Target (BL385) |
|------|-------------------|----------------|
| `memory_remember` | writes global | writes `session-local` (auto-scoped) |
| `memory_recall` | flat global search | scope-hierarchy walk (session → project → global) |
| `memory_list` | global list | `session-local` list for this session |
| `memory_forget` | delete from global | delete from `session-local`; operator promotes to handle global |
| `memory_pin` | global pin | no-op / error in subprocess (global-only semantic) |
| `memory_sweep_stale` | global sweep | blocked in subprocess mode (too destructive) |
| `memory_stats` | global stats | global stats (read-only, safe) |
| `memory_schema_version` | global | global (read-only, safe) |
| `memory_export` | global export | global (read-only, safe) |
| `memory_spellcheck` | global | global (stateless, no side effects) |
| `memory_extract_facts` | global | global (stateless, no side effects) |
| `memory_learnings` | global | scope-hierarchy walk (same as recall) |
| `memory_import` | global import | blocked in subprocess mode (destructive global write) |

Additionally, `memory_remember` gains an explicit `global: true` escape hatch for
the rare case where a subprocess tool needs to deliberately write to the global store.

---

## Architecture

### Subprocess mode detection

```go
// mcp/server.go
func (s *Server) subprocessMode() bool {
    return s.memoryAPI == nil && s.webPort > 0 && s.callerSessionID != ""
}
```

`callerSessionID` is already set via the `--caller-session-id` flag (injected by
the daemon's session manager when it launches `datawatch mcp`). It holds the
FullID of the parent session (e.g. `johnnyjohnny-12cf`).

### New REST endpoint: `POST /api/memory/scopes/save`

The existing `/api/memory/save` writes to the flat global store (no scope). We add
a new endpoint that accepts a `ScopeRef` alongside the content, so subprocess mode
can write scoped without changing the existing global write path.

```
POST /api/memory/scopes/save
Authorization: Bearer <token>

{
  "scope":      "session-local",        // required
  "project":    "/home/user/datawatch", // project dir
  "session_id": "johnnyjohnny-12cf",    // required for session-local
  "persona":    "",                     // for persona-* scopes
  "content":    "the memory text",
  "role":       "user"                  // optional; defaults to ""
}

→ 200 OK
{
  "memory_id": 42,
  "scope": "session-local",
  "session_id": "johnnyjohnny-12cf"
}
```

Route registered in `server.go` alongside the existing scope endpoints:
```go
apiMux.HandleFunc("/api/memory/scopes/", api.handleMemoryScopes)
// already registered; extend the switch in handleMemoryScopes:
case rest == "save" && r.Method == http.MethodPost:
    s.memoryScopeSave(w, r)
```

### memory_remember changes

```go
func (s *Server) handleMemoryRemember(...) {
    text, projectDir := ...
    isGlobal, _ := req.Params.Arguments["global"].(bool)

    if s.subprocessMode() && !isGlobal {
        // Write to session-local scope
        r, ok := s.proxyMemoryPOST("/api/memory/scopes/save", map[string]any{
            "scope":      "session-local",
            "project":    projectDir,
            "session_id": s.callerSessionID,
            "content":    text,
        })
        if ok { return r, nil }
    }

    if s.memoryAPI == nil {
        // Existing global proxy fallback (non-subprocess or subprocess with global:true)
        r, ok := s.proxyMemoryPOST("/api/memory/save", map[string]any{...})
        if ok { return r, nil }
        return mcpsdk.NewToolResultText("Memory not enabled."), nil
    }
    // in-process daemon path (unchanged)
    ...
}
```

Add `global` bool to the `memory_remember` tool schema:
```go
mcpsdk.WithBool("global",
    mcpsdk.Description("Write directly to global memory store (bypass subprocess scope isolation). Default: false."),
)
```

### memory_recall changes

In subprocess mode, proxy to `/api/memory/scopes/recall` with session + project:

```go
if s.subprocessMode() {
    q := url.Values{}
    q.Set("session", s.callerSessionID)
    q.Set("project", projectDir)
    q.Set("top_k", strconv.Itoa(topK))
    r, ok := s.proxyMemoryGET("/api/memory/scopes/recall", q)
    if ok { return r, nil }
}
```

This walks `session-local → project-shared → persona-in-project → persona-global`
and returns merged results with layer attribution. The existing `ScopedRecall`
function already handles this.

### memory_list changes

In subprocess mode, proxy to `/api/memory/scopes/borrow?scope=session-local&session=...&project=...`.

### Blocked tools in subprocess mode

`memory_sweep_stale` and `memory_import` return a clear error in subprocess mode:

```go
if s.subprocessMode() {
    return mcpsdk.NewToolResultError(
        "memory_sweep_stale is blocked in subprocess mode to prevent global data loss. " +
        "Use memory_scope_promote to surface session memories, then sweep from the operator CLI.",
    ), nil
}
```

### memory_pin in subprocess mode

`memory_pin` is only meaningful for global memories (pins affect global recall
ranking). Return an informative message:

```go
if s.subprocessMode() {
    return mcpsdk.NewToolResultText(
        "Pinning is only available for global memories. Use memory_scope_promote to " +
        "move this memory to project-shared first, then pin from an operator session.",
    ), nil
}
```

---

## Files to change

| File | Change |
|------|--------|
| `internal/mcp/server.go` | Add `subprocessMode()` helper |
| `internal/mcp/memory_tools.go` | Update `handleMemoryRemember`, `handleMemoryRecall`, `handleMemoryList`, block `handleMemorySweep` + `handleMemoryImport`, inform `handleMemoryPin` |
| `internal/server/memory_scopes.go` | Add `memoryScopeSave` handler + extend `handleMemoryScopes` switch |
| `internal/server/server.go` | No route changes needed — `/api/memory/scopes/` already catches `/save` |
| `cmd/datawatch/main.go` | Version bump → v8.29.0 |
| `internal/server/api.go` | Version bump → v8.29.0 |
| `CHANGELOG.md` | v8.29.0 entry |
| `README.md` | Badge + release entry |

---

## Phase breakdown

### Phase 1 — REST endpoint + tests

1. Add `memoryScopeSave` in `internal/server/memory_scopes.go`.
   - Decode `{scope, project, session_id, persona, content, role}`.
   - Resolve via `memory.ScopeRef{Scope, Project: project, SessionID: session_id, Persona: persona}`.
   - Write via `backend.Save(projectDir, role, sessionID, content)` (same signature as global save, different key triple).
   - Return `{memory_id, scope, session_id}`.
2. Extend `handleMemoryScopes` switch with `rest == "save" && r.Method == http.MethodPost`.
3. Write unit tests:
   - `TestBL385_ScopeSave_SessionLocal_WritesCorrectKey`
   - `TestBL385_ScopeSave_MissingScope_Returns400`
   - `TestBL385_ScopeSave_MissingSessionID_ForSessionLocal_Returns400`
   - `TestBL385_ScopeSave_ProjectShared_WritesCorrectKey`

### Phase 2 — Subprocess mode routing

1. Add `subprocessMode()` to `mcp/server.go`.
2. Update `handleMemoryRemember`:
   - Add `global` bool to tool schema.
   - In subprocess mode (non-global), proxy to `/api/memory/scopes/save`.
3. Update `handleMemoryRecall`:
   - In subprocess mode, proxy to `/api/memory/scopes/recall?session=...&project=...`.
4. Update `handleMemoryList`:
   - In subprocess mode, proxy to `/api/memory/scopes/borrow?scope=session-local&session=...&project=...`.
5. Block `handleMemorySweep` + `handleMemoryImport` in subprocess mode.
6. Inform `handleMemoryPin` in subprocess mode.
7. Write unit tests:
   - `TestBL385_SubprocessMode_Remember_RoutesTo_ScopeSave`
   - `TestBL385_SubprocessMode_Remember_Global_RoutesTo_GlobalSave`
   - `TestBL385_SubprocessMode_Recall_RoutesTo_ScopedRecall`
   - `TestBL385_SubprocessMode_List_RoutesTo_SessionLocalBorrow`
   - `TestBL385_SubprocessMode_Sweep_Returns_BlockedError`
   - `TestBL385_SubprocessMode_Import_Returns_BlockedError`
   - `TestBL385_SubprocessMode_Pin_Returns_Inform`
   - `TestBL385_NonSubprocessMode_Remember_RoutesTo_GlobalSave` (regression)

### Phase 3 — Docs + version bump

1. Bump both version files to `v8.29.0`.
2. Add CHANGELOG entry.
3. Update README badge.
4. Update `docs/implementation.md` with new `/api/memory/scopes/save` endpoint.

---

## Promotion flow (operator-facing)

After a subprocess session completes, the operator can surface useful memories:

```
# From operator CLI or MCP:
memory_scope_recall session=johnnyjohnny-12cf project=/home/user/datawatch
→ shows what the subprocess wrote

memory_scope_promote memory_id=42 from_scope=session-local from_session=12cf \
    to_scope=project-shared from_project=/home/user/datawatch to_project=/home/user/datawatch
→ promotes to project-shared with breadcrumb provenance

# Or bulk-promote everything from this session:
memory_scope_seed from_scope=session-local from_session=12cf from_project=/home/user/datawatch \
    to_scope=project-shared to_project=/home/user/datawatch
```

No new tools needed — `memory_scope_recall`, `memory_scope_borrow`,
`memory_scope_seed`, and `memory_scope_promote` already cover this.

---

## Out of scope

- Goose subprocess mode — same fix applies automatically once `--caller-session-id`
  is injected. No separate work needed.
- PWA promotion UI — operator can use MCP/CLI tools; a dedicated promotion UI is a
  separate future item.
- Auto-promote on session completion — not in this cut; operator-directed only.
- Changing the scope model itself — BL295 scope hierarchy is the implementation.
- Subprocess `memory_forget` scope targeting — complex (need to know which scope a
  memory ID lives in). Left for a follow-up; for now `memory_forget` remains global
  in subprocess mode (rare operation, low risk).

---

## Test names (full list)

```
TestBL385_ScopeSave_SessionLocal_WritesCorrectKey
TestBL385_ScopeSave_MissingScope_Returns400
TestBL385_ScopeSave_MissingSessionID_ForSessionLocal_Returns400
TestBL385_ScopeSave_ProjectShared_WritesCorrectKey
TestBL385_SubprocessMode_Remember_RoutesTo_ScopeSave
TestBL385_SubprocessMode_Remember_Global_RoutesTo_GlobalSave
TestBL385_SubprocessMode_Recall_RoutesTo_ScopedRecall
TestBL385_SubprocessMode_List_RoutesTo_SessionLocalBorrow
TestBL385_SubprocessMode_Sweep_Returns_BlockedError
TestBL385_SubprocessMode_Import_Returns_BlockedError
TestBL385_SubprocessMode_Pin_Returns_Inform
TestBL385_NonSubprocessMode_Remember_RoutesTo_GlobalSave
```
