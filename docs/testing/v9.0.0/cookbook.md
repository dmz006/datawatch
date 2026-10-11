# E2E Test Cookbook — v9.0.0

**Version**: v9.0.0  
**Sprint**: T45 + T46 + T47 — Memory Lifecycle, Executor Resilience, Per-Story LLM, Per-Guardrail Approve, Parallel LLM Execution, B102–B106 UI Features  
**Stories**: TS-680–TS-705 (26 tests)  
**Last Run**: 2026-09-16  
**Pass Rate**: — (T47 stories added 2026-09-16)  
**Status**: 📋 T47 added; prior sprints green

---

## T45 Results

| TS# | Description | Status | Notes |
|---|---|---|---|
| TS-680 | POST /api/memory/scopes/save writes memory to named scope | 📋 planned | — |
| TS-681 | POST /api/memory/scopes/delete removes scoped entry by id | 📋 planned | — |
| TS-682 | GET /api/memory/scopes/inventory returns scope row counts | 📋 planned | — |
| TS-683 | POST /api/memory/scopes/archive-import accepts valid request | 📋 planned | — |
| TS-684 | GET /api/autonomous/prds/{id}/memory-report returns prd_id field | 📋 planned | — |
| TS-685 | DELETE /api/autonomous/prds/{id}?memory_strategy=archive deletes with archive | 📋 planned | — |
| TS-686 | GET /api/memory/scopes/recall with prd_id param returns 200 | 📋 planned | — |
| TS-687 | POST /api/autonomous/prds with memory_seed.from_prds accepted | 📋 planned | — |
| TS-688 | POST /api/sessions/{id}/guardrail/{name}/approve returns 200 or 404 | 📋 planned | — |
| TS-689 | executor_resume_test.go: 4 TestExecutorResume tests pass | 📋 planned | — |
| TS-690 | PATCH /api/autonomous/prds/{id} set_story_llm field accepted | 📋 planned | — |
| TS-691 | scoped save + recall round-trip: content written then read back | 📋 planned | — |
| TS-692 | autonomous_prd_set_story_llm MCP tool exists and is callable | 📋 planned | — |
| TS-693 | memory_scope_recall MCP tool accepts prd_id param | 📋 planned | — |
| TS-694 | memory-scope PWA tile visible on dashboard (memory enabled) | 📋 planned | conflict:pwa |

---

## Feature Coverage

### Memory Scope API (BL385 — v8.29.0)

| Surface | Story | Expected |
|---|---|---|
| REST | TS-680 | POST /api/memory/scopes/save |
| REST | TS-681 | POST /api/memory/scopes/delete |
| REST | TS-686 | GET /api/memory/scopes/recall with prd_id |
| REST | TS-691 | save + recall round-trip |
| MCP | TS-693 | memory_scope_recall with prd_id |

### Memory Lifecycle (BL386 — v8.30.0)

| Surface | Story | Expected |
|---|---|---|
| REST | TS-682 | GET /api/memory/scopes/inventory |
| REST | TS-683 | POST /api/memory/scopes/archive-import |
| REST | TS-684 | GET /api/autonomous/prds/{id}/memory-report |
| REST | TS-685 | DELETE PRD with memory_strategy=archive |

### Automata Memory Integration (BL387 — v8.31.0–v8.33.0)

| Surface | Story | Expected |
|---|---|---|
| REST | TS-687 | memory_seed.from_prds cross-seeding accepted |
| PWA | TS-694 | memory-scope dashboard tile renders |

### Executor Resilience (v8.33.8)

| Surface | Story | Expected |
|---|---|---|
| Unit | TS-689 | 4 TestExecutorResume tests pass |

### Per-Guardrail Approval (v8.33.3)

| Surface | Story | Expected |
|---|---|---|
| REST | TS-688 | POST guardrail/{name}/approve — 200 or 404 |

### Per-Story LLM Config (BL381 — v8.28.0)

| Surface | Story | Expected |
|---|---|---|
| REST | TS-690 | set_story_llm action accepted |
| MCP | TS-692 | autonomous_prd_set_story_llm tool callable |

---

## Run Commands

```bash
# Full T45 sprint
bash scripts/run-tests.sh --sprint=T45

# Memory features only
bash scripts/run-tests.sh --feature=memory --sprint=T45

# Skip PWA (no Chromium)
bash scripts/run-tests.sh --sprint=T45 --skip-conflict=pwa

# Single story
bash scripts/run-tests.sh --story=TS-680
```

---

## T46 Results — Parallel LLM Execution (BL370)

| TS# | Description | Status | Notes |
|---|---|---|---|
| TS-695 | BL370 parallel Automata across two Ollama compute nodes; simultaneous node stats | 📋 planned | Requires TEST_OLLAMA2_HOST (second Ollama); skips if unreachable |

---

## Feature Coverage (T46)

### Parallel Task Execution (BL370 — v8.26.0)

| Surface | Story | Expected |
|---|---|---|
| REST | TS-695 | PRD with max_concurrent_tasks=2, two stories routed to distinct compute nodes; both reach terminal state |
| REST | TS-695 | GET /api/compute/nodes/{name}/health for both nodes simultaneously returns valid JSON mid-run |
| REST | TS-695 | GET /api/compute/nodes/{name}/detail for both nodes simultaneously returns Ollama stats |

---

## Run Commands (T46)

```bash
# Full T46 sprint (needs two Ollama servers)
TEST_OLLAMA_HOST=http://ollama-1:11434 TEST_OLLAMA2_HOST=http://ollama-2:11434 \
  bash scripts/run-tests.sh --sprint=T46

# Single story
bash scripts/run-tests.sh --story=TS-695
```

---

## Coverage Gaps (pending live-LLM tests)

BL370 parallel task execution is covered by TS-695 (T46).

The following features still have unit/API tests but lack live-LLM e2e coverage:

| Feature | What's missing | Conflict tag needed |
|---|---|---|
| BL387 auto-report on PRDCompleted | PRD must reach completed state with memory backend live | conflict:llm |
| BL387 verifier-finding write to prd-shared | PRD must run to failed task with verifier | conflict:llm |
| BL386 warm-start seeding | PRD must start second run to trigger seed | conflict:llm |
| BL381 per-story spawn with model set | Story must actually execute on a specific backend | conflict:llm |
| v8.33.8 boot-resume on real daemon | Daemon restart then PRD advancement | conflict:llm |

Add these as T47 stories (DW_MAJOR=1 gate) when a live LLM backend is available in CI.

---

## T47 Stories — B102–B106 UI Features (v8.33.27–v8.33.32)

| TS# | Description | Feature | Status | Notes |
|---|---|---|---|---|
| TS-696 | B102: session detail returns valid JSON after hook Stop (files_touched shape) | B102 files_touched | 📋 planned | No LLM needed |
| TS-697 | B106: GET /api/files/download?inline=1 returns content without attachment header | B106 file viewer | 📋 planned | Requires root_path-accessible file |
| TS-698 | B106: GET /api/files/download (no inline) returns Content-Disposition: attachment | B106 file viewer | 📋 planned | — |
| TS-699 | B103: PRD decompose/stream SSE endpoint reachable | B103 live updates | 📋 planned | No LLM needed |
| TS-700 | B104: PRD detail has status field | B104 status parity | 📋 planned | — |
| TS-701 | B105: GET /api/autonomous/prds includes completed items; ?status= filter accepted | B105 filter badges | 📋 planned | — |
| TS-702 | B102: PRD stories/tasks have files field when LLM decompose ran | B102 files chips | 📋 planned | conflict:llm for full check |
| TS-703 | B104/B105: automata status locale keys present in all 5 bundles | B104/B105 locale | 📋 planned | Keys may not yet exist |
| TS-704 | B106: _showFileViewer and _fileChip functions present in app.js | B106 JS surface | 📋 planned | Static check |
| TS-705 | B102: files_touched paths are absolute (start with /) | B102 absolute paths | 📋 planned | — |

### Run Commands (T47)

```bash
# Full T47 sprint
bash scripts/run-tests.sh --sprint=T47

# Individual stories
bash scripts/run-tests.sh --story=TS-697
bash scripts/run-tests.sh --story=TS-704

# All B102-B106 coverage
bash scripts/run-tests.sh --group=b102-files-touched-v9
bash scripts/run-tests.sh --group=b103-live-updates-v9
bash scripts/run-tests.sh --group=b104-status-parity-v9
bash scripts/run-tests.sh --group=b105-filter-badges-v9
bash scripts/run-tests.sh --group=b106-file-viewer-v9
```

---

## B115 backfill — BL406 + BL407, v9.0.3–v9.0.21 (2026-10-10)

**B115**: zero e2e stories existed for any of the 21 releases between
the v9.0.0 tag (2026-10-09) and v9.0.21 (2026-10-10) — confirmed via
`git log --diff-filter=A -- scripts/test-stories/` (last story file
added was TS-786, 2026-10-08, before the tag) and a full-text grep of
every existing story for `BL406`/`BL407`/the v9.0.1-21 version strings
(zero matches). See `docs/plans/README.md`'s Open/Completed Bugs
section for the full writeup. This section backfills the subset that's
safely REST-testable without a live LLM, a real GitHub write, or a real
docker/k8s cluster spin-up; the "Known gaps" table below documents what
still has zero e2e coverage and why, rather than leaving that implicit.

| TS# | Description | Release | Status | Notes |
|---|---|---|---|---|
| TS-787 | BL406 Phase 0: `rules_file`/`context_file`/`upstream_repos` round-trip via PUT/GET /api/config | v9.0.11 | ✅ pass | — |
| TS-788 | BL406 Phase 1: ProjectRulesScanner `content` rule fires a real finding against a file on disk | v9.0.12 | ✅ pass | mutates+restores global scan config |
| TS-789 | BL406 Phase 2: default parity-inheritance rule flags a story that drops a named surface | v9.0.13 | ✅ pass | conflict:llm — needs one real decompose to reach `needs_review` |
| TS-790 | BL406 Phase 3: `project-rules-scan` present in the PWA Guardrails dropdown | v9.0.14 | ✅ pass | static check |
| TS-791 | v9.0.3: `loadAboutOrphanedTmux` uses `apiFetch`, not a bare `fetch()` (federation-aware) | v9.0.3 | ✅ pass | static check |
| TS-792 | BL406 Phase 5 (B111): `set_guided_mode` live-applies `guided_mode_source` | v9.0.17 | ✅ pass | — |
| TS-793 | BL406 Phase 5 (B112): opt-in `scope_drift` rule flags a code-creation task under a doc-only PRD | v9.0.17 | ✅ pass | conflict:llm + conflict:selfconfig |
| TS-794 | v9.0.16: `notify_exclude` round-trip via PUT/GET /api/config | v9.0.16 | ✅ pass | — |
| TS-795 | B113: `sast_enabled` round-trips via GET/PUT /api/autonomous/scan/config | v9.0.11 | ✅ pass | restart-persistence half already verified live at ship time, not repeated here |

Verified together, serial lane, against a real sandbox daemon
(`bash scripts/run-tests.sh --stories=TS-787,TS-788,TS-789,TS-790,TS-791,TS-792,TS-793,TS-794,TS-795 --serial`):
9 passed, 0 failed, 0 skipped.

### Known gaps — deliberately not backfilled

| Release | Gap | Why deferred |
|---|---|---|
| v9.0.15 (BL406 Phase 4) | upstream issue-filing action (`file_upstream_issue`) | would file a real GitHub issue against a real repo if run automatically — same class of risk this suite never takes for the pre-existing auto-PR-on-completion behavior either |
| v9.0.18 (BL407 Phase 0 / B114) | cluster-dispatch reaches a terminal state instead of hanging — the actual bug class B114 fixed | needs a real worker container image this harness doesn't have yet (docker *is* available on this host, but no datawatch worker image exists to pull/build for it) — filed as its own follow-up, not silently skipped |
| v9.0.19–21 (BL407 Phases 1-3) | local git-worktree isolation, completion push+PR, cluster-mode git | `PRD.Git.AutoPR`/`BaseBranch` have no REST/MCP/CLI/PWA exposure yet — that's this same plan's own Phase 5. Covered today only at the Go unit/integration level (`internal/autonomous/*_test.go`, real bare-remote git round trips) |
| v9.0.7 / v9.0.8 | boot self-heal of a `StateFailed` session; usage-tracker checkpoint surviving a restart | both require restarting the shared sandbox daemon mid-suite, which would affect every other concurrently-running story — same deferral precedent as this cookbook's existing "boot-resume on real daemon" conflict:llm item above |
| v9.0.10 | `imap_mcp.to` config field | YAML-only, no REST/MCP/CLI/comm/PWA exposure (tracked separately as `BL404`) — nothing for an e2e story to call |
| v9.0.1, v9.0.2, v9.0.4, v9.0.5, v9.0.6, v9.0.9 | container build pin, Dockerfile GO_VERSION drift check, Android-beta docs wording, CVE suppression, `datawatch-channel` release archive fix, README staleness | build/release/CI/docs-only — no REST/MCP/CLI/PWA surface exists to e2e-test; v9.0.1/v9.0.2 already have their own CI-level regression gate (`scripts/check-dockerfile-go-versions.sh`, wired into `ci.yaml`/`release.yaml`) |

### Run commands (B115 backfill)

```bash
bash scripts/run-tests.sh --stories=TS-787,TS-788,TS-789,TS-790,TS-791,TS-792,TS-793,TS-794,TS-795
```
