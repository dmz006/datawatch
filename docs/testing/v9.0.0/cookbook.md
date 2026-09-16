# E2E Test Cookbook — v9.0.0

**Version**: v9.0.0  
**Sprint**: T45 + T46 — Memory Lifecycle, Executor Resilience, Per-Story LLM, Per-Guardrail Approve, Parallel LLM Execution  
**Stories**: TS-680–TS-695 (16 tests)  
**Last Run**: —  
**Pass Rate**: — (0/15)  
**Status**: 📋 Ready to run

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

Add these as T46 stories (DW_MAJOR=1 gate) when a live LLM backend is available in CI.
