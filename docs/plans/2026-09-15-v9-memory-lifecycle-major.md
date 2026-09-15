# v9.0.0 — Memory Lifecycle Complete (Major Release)

**Status:** Draft  
**Target release:** v9.0.0  
**Prerequisites:** BL385 (v8.29.0) · BL386 (v8.30.0) · BL387 (v8.33.0)  
**Filed:** 2026-09-15  
**Author:** dmz006

---

## Purpose

v9.0.0 is an operator-designated major release milestone. It contains no new
features beyond what shipped in v8.29.0–v8.33.0. Its purpose is:

1. Full end-to-end validation of the memory lifecycle trilogy (BL385+386+387)
2. Performance testing at realistic scale
3. Backward-compatibility audit (memory API surface, scope model, existing recall behavior)
4. Complete documentation pass
5. Release as a stable, long-term-support-eligible milestone

Per AGENT.md: "Breaking changes (user must explicitly request) = major bump."
This major is operator-designated to mark a complete feature milestone. Any
breaking changes identified during the backward-compat audit must be resolved
before the tag is cut.

---

## Prerequisites checklist

Before v9.0.0 work begins, verify all are complete and live:

- [ ] BL385 (v8.29.0): scope model (prd-shared/story-shared), subprocess isolation, scope_save, scope_delete, executor injection
- [ ] BL386 (v8.30.0): warm-start seeding, harvest, archive-on-delete, handoff tool, PRD report, scope inventory, scope TTL
- [ ] BL387 (v8.31.0): verifier memory, child PRD inheritance
- [ ] BL387 (v8.32.0): decomposer enrichment, cross-PRD seeding
- [ ] BL387 (v8.33.0): auto-report on completion, PWA memory stats tile
- [ ] All tests from BL385/386/387 pass on `main` with zero failures
- [ ] All `docs/testing-tracker.md` rows for BL385/386/387 have Tested=Yes
- [ ] At least the critical e2e scenarios (E1, E5 from BL387) have Validated=Yes

---

## Phase 1 — Integration e2e suite

Write a comprehensive end-to-end test suite that exercises the full lifecycle
from first PRD spawn to deletion, across all 6 scope layers.

### Suite structure

`internal/autonomous/v9_lifecycle_e2e_test.go`

The suite uses an in-process daemon (test server + real SQLite memory backend +
fake LLM that returns controlled outputs). No external services required.
A separate `make e2e-memory-live` target runs the same suite against a real
running daemon with a real model.

### Test cases

**TC1: Full PRD lifecycle with memory — happy path**
```
1. Create project; write 5 project-shared memories
2. Create PRD with memory_seed.enabled=true, memory_harvest.enabled=true,
   memory_harvest.promote_to=prd-shared
3. Decompose → assert decomposer prompt contains prior-context block
4. Run 3 tasks across 2 stories:
   - Task 1: writes 3 session-local memories
   - Task 2 (same story): assert session seeded with story-shared from task 1 harvest
   - Task 3 (different story): assert session seeded with prd-shared from earlier harvest
5. Complete PRD → assert memory_report generated on PRD record
6. scope_inventory → verify prd-shared/story-shared counts correct
7. Delete PRD with memory_strategy=archive → verify memories promoted to project-shared
8. scope_inventory after delete → prd-shared/story-shared counts = 0
9. project-shared count increased by archived memories
```

**TC2: Verifier feedback loop with memory**
```
1. PRD with memory_seed (role_filter=verifier-finding)
2. Task fails verification with 2 findings
3. Assert prd-shared has 2 verifier-finding memories
4. Retry spawns → assert seeded with verifier-finding memories
5. Retry passes → no new verifier-findings
6. Retry session's harvest: no verifier-findings promoted upward
```

**TC3: Cross-PRD knowledge chain**
```
1. PRD A runs and completes — accumulates 8 prd-shared memories
2. PRD B: from_prds=[A.id], memory_seed.enabled=true
3. First task of B: assert prd-shared has inherited A's memories
4. Decomposer of B: assert prior-context block includes A's memories
5. Child PRD C spawned from B task: assert C inherits B's + A's memories (depth-2)
```

**TC4: Subprocess isolation — opencode-like subprocess**
```
1. Start daemon; write 20 global memories
2. Spawn `datawatch mcp --caller-session-id=X --caller-prd-id=Y --caller-story-id=Z`
3. Call memory_remember (no scope) → assert writes to session-local, NOT global
4. Call memory_recall → assert returns hierarchy walk (session+story+prd+project)
5. Call memory_sweep_stale → assert returns blocked error
6. Global memory count = 20 (unchanged)
7. Session-local count for session X = 1 (the write from step 3)
```

**TC5: Archive-on-delete round-trip**
```
1. PRD with 10 prd-shared memories (roles: learning=5, noise=5)
2. Delete with strategy=archive, role_filter=[learning]
3. Assert: prd-shared count = 0
4. Assert: project-shared count increased by 5 (learning memories only)
5. Assert: breadcrumb on archived memories contains prd_id and promoted_at
6. Noise memories: deleted (not in any scope)
```

**TC5b: Archive import — seed new PRD from deleted PRD's archive**
```
1. PRD A: accumulates 10 prd-shared memories (roles: learning=7, noise=3)
2. Delete PRD A with strategy=archive, role_filter=[learning]
3. Assert: 7 archived memories in project-shared with breadcrumb archived_from_prd=A.id
4. Create PRD B; call memory_archive_import source_prd_id=A.id target_prd_id=B.id
   role_filter=[learning] max=10
5. Assert: B's prd-shared has 7 memories with breadcrumb imported_from_archive=A.id
6. Verify dry_run=true returns count=7 without writing
7. PRD B's first task auto-seeds from prd-shared → task sees A's learnings
8. Call archive_import a second time → assert skipped_duplicate=7, seeded=0 (dedup)
```

**TC6: Scope TTL sweep**
```
1. Write 5 session-local memories with timestamps set to 35 days ago (test hook)
2. Write 3 project-shared memories with timestamps set to 35 days ago
3. Configure: session_local_days=30, project_shared_days=730
4. Run scope TTL sweep
5. Assert: 5 session-local memories deleted
6. Assert: 3 project-shared memories untouched (730-day TTL not exceeded)
```

**TC7: Backward-compatibility — flat global recall unchanged**
```
1. Non-subprocess session (memoryAPI set, webPort 0)
2. Call memory_remember → assert writes to global flat store
3. Call memory_recall → assert returns flat global results (no scope walk)
4. Assert: no regression in existing recall behavior
5. Assert: 13 memory tools work identically to v8.28.7 behavior for non-subprocess sessions
```

**TC8: Concurrent tasks — parallel story execution**
```
1. PRD with 2 stories, each with 2 tasks; MaxConcurrentTasks=4
2. All 4 tasks run in parallel
3. Task A1 writes to story-shared for story 1
4. Task A2 (same story, parallel): assert can recall A1's story-shared memory
   (note: A1 may not have written it yet — eventual consistency is acceptable;
   assert it's there after both complete)
5. Task B1 writes to prd-shared
6. Task B2 (different story): assert can recall B1's prd-shared memory
7. No cross-story contamination: A1's story-shared NOT visible in B1's story scope
```

**Phase 1 checklist:**

- [ ] `internal/autonomous/v9_lifecycle_e2e_test.go` created
- [ ] All 9 test cases implemented and pass (TC1–TC8 + TC5b archive import)
- [ ] In-process test setup: real SQLite backend + fake LLM output control
- [ ] `make e2e-memory-live` target added (requires running daemon)
- [ ] CI: e2e suite added to test matrix (allowed-to-fail in CI, required locally)
- [ ] `docs/testing-tracker.md`: v9.0.0 e2e suite section, all 8 TCs = Tested=Yes

---

## Phase 2 — Performance testing

### Baseline scenarios

- **Memory scale**: 10,000 memories in project-shared + 500 per session-local for 20 sessions. Verify `ScopedRecall` completes in < 500ms.
- **Scope seed**: seed 100 memories from project-shared → session-local. Verify < 200ms.
- **Decomposer enrichment**: query project-shared with 10,000 entries; top-15 recall for decomposer prompt. Verify < 300ms.
- **PRD report**: aggregate 50 sessions × 20 memories each. Verify < 2s.
- **Scope inventory**: enumerate scopes on project with 10 PRDs × 5 stories × 3 tasks. Verify < 1s.

### Benchmark test file

`internal/memory/v9_perf_bench_test.go` — `go test -bench=.` tests.

**Phase 2 checklist:**

- [ ] Benchmark file created with 5 scenarios
- [ ] All benchmarks pass performance targets on the CI runner
- [ ] Document baseline numbers in `docs/testing-tracker.md`
- [ ] If any benchmark fails target: file bug, fix before v9.0.0 tag
- [ ] PostgreSQL backend benchmarks run separately if available

---

## Phase 3 — Backward-compatibility audit

### Audit checklist

- [ ] `GET /api/memory/search`, `/api/memory/list`, `/api/memory/save`, `/api/memory/delete` — responses unchanged for non-scoped requests
- [ ] `memory_remember`, `memory_recall`, `memory_list`, `memory_forget` — tool behavior unchanged for non-subprocess sessions
- [ ] `memory_sweep_stale` with no `scope` param — sweeps globally as before
- [ ] `memory_stats` — existing fields still present; new `scopes` field is additive
- [ ] `autonomous_prd_get` — new `memory_report`, `memory_report_at` fields are optional; absent when not generated
- [ ] `SpawnRequest` — `MemorySeed` field is zero-value-safe (disabled by default)
- [ ] `ScopeRef.Resolve()` — existing 5 scope types return same tuples as before
- [ ] `AllScopesTopDown` — extended list does not break existing `ScopedRecall` callers that pass a subset of layers
- [ ] `POST /api/memory/scopes/archive-import` — new endpoint; existing PRDs with no archives return `{seeded: 0}` (not an error)
- [ ] Delete PRD without `memory_strategy` param — behavior unchanged (keep, no sweep)
- [ ] Delete session without `memory_strategy` — behavior unchanged
- [ ] Non-PRD subprocess (no `--caller-prd-id`) — memory_remember still writes to session-local; prd/story layers silently skipped in recall

**Phase 3 checklist:**

- [ ] All audit items verified by test or code inspection
- [ ] No undocumented breaking changes found
- [ ] If breaking changes identified: document in CHANGELOG under "Breaking Changes" and bump justification added

---

## Phase 4 — Documentation complete

**Phase 4 checklist:**

- [ ] `docs/implementation.md`: all new REST endpoints from BL385/386/387 documented
  - `POST /api/memory/scopes/save`
  - `POST /api/memory/scopes/delete`
  - `GET  /api/memory/scopes/inventory`
  - `GET  /api/memory/scopes/recall` — updated with prd_id/story_id params
  - `POST /api/memory/scopes/archive-import`
  - `POST /api/autonomous/prds/{id}/memory-report`
- [ ] `docs/setup.md`: memory_seed / memory_harvest config examples for PRDs
- [ ] `docs/operations.md`: scope TTL config, auto-sweep setup, archive-on-delete guidance
- [ ] All new MCP tools documented in tool descriptions (self-documenting via `WithDescription`)
- [ ] `docs/plan-attribution.md`: BL385/386/387 entries if any external inspiration
- [ ] `docs/testing-tracker.md`: all BL385/386/387 rows complete (Tested + Validated)

---

## Phase 5 — Full test suite green + release

**Phase 5 checklist:**

- [ ] `rtk go test ./...` — all tests pass, zero failures
- [ ] e2e suite: `make e2e-memory-live` against a fresh daemon — all 8 TCs pass
- [ ] ZAP scan: no new WARN or FAIL alerts introduced by new endpoints
- [ ] `scripts/tidy-plans.sh` — no stale plans in `docs/plans/`
- [ ] `scripts/release-smoke.sh` — passes
- [ ] Both version files → `v9.0.0`
- [ ] CHANGELOG v9.0.0 entry: summarize all 3 BLs + reference BL385/386/387 plan docs
- [ ] README badge → v9.0.0
- [ ] README recent-releases section: add v9.0.0 as major milestone
- [ ] Tag: `git tag v9.0.0` → CI pipeline handles goreleaser + containers
- [ ] **Do NOT** run `make cross` or `gh release create` manually

---

## Release notes draft (v9.0.0)

```
## v9.0.0 — Memory Lifecycle Complete

v9.0.0 is a major milestone release completing the three-BL memory lifecycle
trilogy (BL385, BL386, BL387). No new features beyond v8.33.0; this release
delivers full end-to-end validation, performance benchmarks, backward-compat
audit, and documentation for the complete memory lifecycle system.

### What's new (cumulative from v8.29.0)

**Scope model (BL385 — v8.29.0)**
- Two new scope layers: prd-shared and story-shared
- Subprocess MCP mode (opencode, Goose) defaults to session-local writes
- Scoped recall walks full 6-layer hierarchy
- New REST: POST /api/memory/scopes/save, POST /api/memory/scopes/delete
- Executor injects --caller-prd-id, --caller-story-id at task spawn

**Memory lifecycle (BL386 — v8.30.0)**
- Warm-start seeding: executor pre-seeds task sessions from project/prd/story scopes
- Harvest: auto-promote session-local learnings on task completion
- Archive-on-delete: PRD/session delete with promote-then-purge strategy
- Archive import: seed a new PRD from a prior PRD's archived memories (memory_archive_import tool + REST)
- memory_handoff tool for explicit task-to-task context passing
- memory_prd_report tool for per-PRD learning summary
- Scope inventory: enumerate all scopes with memory data
- Scope-aware TTL sweep with optional daily auto-sweep

**PRD integration (BL387 — v8.31.0–v8.33.0)**
- Verifier writes structured findings to prd-shared; retry tasks auto-seeded
- Child PRDs inherit parent prd-shared memories at instantiation
- Decomposer queries project-shared memory before generating tasks
- Cross-PRD seeding: declare from_prds to inherit prior-PRD learnings
- Auto-generated PRD memory report on completion
- PWA: memory stats tile on PRD status card
```

---

## Milestone summary

```
v8.28.7 (current)     subprocess memory proxy fix + searxng timeout
v8.29.0  BL385        scope mechanics: prd-shared/story-shared layers, subprocess isolation
v8.30.0  BL386        lifecycle policies: seeding, harvest, archive, report, inventory, TTL
v8.31.0  BL387 P1     verifier memory, child PRD inheritance
v8.32.0  BL387 P2     decomposer enrichment, cross-PRD seeding
v8.33.0  BL387 P3     auto-report on completion, PWA memory stats
v9.0.0               major milestone: e2e suite, perf benchmarks, compat audit, docs complete
```
