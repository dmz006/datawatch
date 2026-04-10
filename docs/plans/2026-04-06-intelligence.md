# Intelligence Features Plan

**Date:** 2026-04-06
**Priority:** low-medium
**Effort:** 3-6 weeks total
**Category:** intelligence

---

## Overview

This plan covers all intelligence-category backlog items. They share common
infrastructure (embeddings, vector storage, LLM orchestration) so implementing
them together avoids redundant work.

### Dependency graph

```
BL23 (episodic memory)  ←── foundation for BL32, BL36
BL32 (semantic search)  ←── depends on BL23 embedding infrastructure
BL36 (task learnings)   ←── depends on BL23 storage, BL32 retrieval

BL24 (task decomposition) ←── depends on BL15 (session chaining, open)
BL39 (circular deps)      ←── depends on BL24 pipeline graph
BL25 (verification)       ←── depends on BL24 task completion signals
BL28 (quality gates)      ←── depends on BL24 pre/post hooks
```

### Implementation order

| Phase | Items | Effort | Dependencies |
|-------|-------|--------|-------------|
| 1 | BL23 — Episodic memory | 1 week | None |
| 2 | BL32 — Semantic search | 2-3 days | BL23 |
| 3 | BL36 — Task learnings | 1-2 days | BL23, BL32 |
| 4 | BL24 — Autonomous task decomposition | 1 week | F15 (session chaining) |
| 5 | BL39 — Circular dependency detection | 2-3 hours | BL24 |
| 6 | BL25 — Independent verification | 2-3 days | BL24 |
| 7 | BL28 — Quality gates | 2-3 days | BL24 |

---

## Phase 1: BL23 — Episodic Memory

**Goal:** Persistent vector-indexed conversation memory per project. Auto-retrieve
relevant context when starting new tasks. `remember` and `recall` commands from
comm channels.

### Architecture

```
internal/memory/
  store.go      — SQLite + vector storage (sqlite-vec or custom cosine similarity)
  embeddings.go — embedding provider (Ollama, OpenAI, or local model)
  retriever.go  — similarity search with configurable top-k
```

### 1.1 Embedding provider

**File:** `internal/memory/embeddings.go`

Interface:
```go
type Embedder interface {
    Embed(ctx context.Context, text string) ([]float32, error)
    Dimensions() int
}
```

Implementations:
- `OllamaEmbedder` — calls `POST /api/embeddings` on local Ollama (default, free)
- `OpenAIEmbedder` — calls OpenAI embeddings API (optional, better quality)

Config:
```yaml
memory:
  enabled: true
  embedder: ollama          # or "openai"
  ollama_model: nomic-embed-text
  openai_model: text-embedding-3-small
  dimensions: 768
  top_k: 5                  # results to retrieve
  auto_save: true           # save session summaries on completion
```

### 1.2 Vector store

**File:** `internal/memory/store.go`

SQLite database per project dir: `{data_dir}/memory/{project_hash}.db`

Tables:
```sql
CREATE TABLE memories (
  id INTEGER PRIMARY KEY,
  session_id TEXT,
  project_dir TEXT,
  content TEXT NOT NULL,
  summary TEXT,
  role TEXT DEFAULT 'session',  -- session, manual, learning
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  embedding BLOB               -- float32 array serialized
);
CREATE INDEX idx_memories_project ON memories(project_dir);
```

Operations:
- `Save(projectDir, content, role, embedding)` — insert new memory
- `Search(projectDir, queryEmbedding, topK)` — cosine similarity search
- `Delete(id)` — remove a memory
- `ListRecent(projectDir, n)` — last N memories

### 1.3 Auto-save on session completion

**File:** `internal/memory/retriever.go`, `cmd/datawatch/main.go`

Hook into `SetOnSessionEnd`:
1. Read session output (last 200 lines from output.log)
2. Generate a summary using the LLM (short prompt: "Summarize what was done")
3. Embed the summary
4. Store in memory DB

### 1.4 Auto-retrieve on session start

Hook into session launch:
1. Embed the task text
2. Search memory DB for top-K similar memories
3. Prepend context to the system prompt or inject as a preamble

### 1.5 Commands

Router commands:
- `remember: <text>` — manually save a memory for the current project
- `recall: <query>` — search memories by meaning, return top matches
- `memories` — list recent memories for the project
- `forget: <id>` — delete a memory

---

## Phase 2: BL32 — Semantic Search Across Sessions

**Goal:** Vector-indexed session output and conversation history. `recall`
command searches past sessions by meaning, not just text matching.

### Architecture

Extends Phase 1 infrastructure:
- Index session outputs on completion (not just summaries)
- Chunk large outputs into ~500 token segments for granular retrieval
- Add session metadata (ID, backend, task, timestamp) to search results

### 2.1 Output chunking

**File:** `internal/memory/chunker.go`

Split session output into overlapping chunks:
- ~500 tokens per chunk, 50 token overlap
- Each chunk stored as a separate memory row with session_id reference
- Tag with `role = "output_chunk"`

### 2.2 Enhanced recall

Extend `recall` command to:
- Search across all session outputs and summaries
- Return results with session context: `[session-id] [date] [match snippet]`
- Filter by date range: `recall since:2026-04-01: deployment errors`

---

## Phase 3: BL36 — Task Learnings Capture

**Goal:** After each completed session, extract key decisions and learnings.
Searchable via `learnings` command. Builds institutional knowledge.

### Architecture

Extends Phase 1+2:

### 3.1 Learning extraction

On session completion:
1. Send session summary + diff to LLM with prompt:
   "What are the key decisions, patterns, or gotchas from this task?
    Format as a bullet list of learnings."
2. Store each learning as a separate memory with `role = "learning"`
3. Tag with project, backend, and task category

### 3.2 Commands

- `learnings` — list recent learnings for the project
- `learnings search: <query>` — semantic search across learnings

---

## Phase 4: BL24 — Autonomous Task Decomposition

**Goal:** `complex: <task>` breaks large tasks into atomic subtasks, dispatches
to parallel workers, verifies each independently.

**Depends on:** F15 (session chaining) — must be implemented first.

### Architecture

```
internal/decompose/
  planner.go    — LLM-based task decomposition
  pipeline.go   — DAG execution engine
  worker.go     — subtask session management
```

### 4.1 Task planner

**File:** `internal/decompose/planner.go`

1. Send the complex task to an LLM with a structured prompt:
   "Break this task into atomic, independently testable subtasks.
    Output as JSON: [{id, title, depends_on: [ids], test_criteria}]"
2. Parse the DAG of subtasks
3. Validate: no circular deps, all dependencies exist, leaf tasks are atomic

### 4.2 Pipeline executor

**File:** `internal/decompose/pipeline.go`

DAG executor:
- Track subtask states: pending → running → completed/failed
- Launch subtasks whose dependencies are all completed
- Parallel execution of independent subtasks
- Configurable max parallel workers (default: 3)
- On subtask failure: retry once, then mark pipeline failed

### 4.3 Router command

- `complex: <task>` — decompose and execute
- `pipeline status` — show DAG execution state
- `pipeline cancel` — cancel all running subtasks

### 4.4 Web UI

- Pipeline view in session detail: DAG visualization (simple linear list with status icons)
- Subtask cards with expandable output

---

## Phase 5: BL39 — Circular Dependency Detection

**Goal:** Prevent deadlocks in task pipelines from Phase 4.

### Implementation

**File:** `internal/decompose/pipeline.go`

Standard cycle detection on the subtask DAG:
1. Topological sort using Kahn's algorithm
2. If sort fails (remaining nodes with edges), report the cycle
3. Run validation before pipeline execution starts
4. Return error with the cycle path: "Circular dependency: A → B → C → A"

Effort: 2-3 hours — pure graph algorithm, no external dependencies.

---

## Phase 6: BL25 — Independent Verification

**Goal:** Each completed task verified by a separate LLM context for security,
logic errors, and correctness. Fail-closed: rejected code is not committed.

**Depends on:** BL24 (subtask completion signals)

### Architecture

### 6.1 Verification agent

**File:** `internal/decompose/verifier.go`

After each subtask completes:
1. Collect the git diff produced by the subtask
2. Send to a separate LLM session (different from the worker) with prompt:
   "Review this diff for: security vulnerabilities, logic errors, missing edge cases,
    test coverage gaps. Respond with APPROVE or REJECT: <reason>"
3. Parse response:
   - APPROVE → mark subtask verified, allow commit
   - REJECT → mark subtask failed, report reason, trigger retry or escalate

### 6.2 Config

```yaml
session:
  verification:
    enabled: true
    backend: claude-code      # which backend to use for verification
    profile: reviewer         # optional profile override
    auto_reject_commit: true  # block git commit on rejection
```

---

## Phase 7: BL28 — Quality Gates

**Goal:** Run test suite before and after each task. Detect regressions.
Block completion if tests regress.

### Architecture

### 7.1 Test runner

**File:** `internal/decompose/quality.go`

Before task starts:
1. Run configured test command (e.g., `go test ./...`, `npm test`)
2. Capture baseline: pass count, fail count, failing test names

After task completes:
1. Run same test command
2. Compare with baseline:
   - New failures → REGRESSION (block)
   - Same failures → PREEXISTING (warn, allow)
   - Fewer failures → IMPROVEMENT (celebrate)

### 7.2 Config

```yaml
session:
  quality_gates:
    enabled: true
    test_command: "go test ./..."
    timeout: 300              # seconds
    block_on_regression: true
    ignore_flaky: []          # test names to ignore
```

### 7.3 Integration with BL24

When used with autonomous decomposition:
- Baseline captured once before the pipeline starts
- Each subtask's diff is tested incrementally
- Cumulative regression check at pipeline end

---

## Configuration Summary

```yaml
memory:
  enabled: false
  embedder: ollama
  ollama_model: nomic-embed-text
  dimensions: 768
  top_k: 5
  auto_save: true

session:
  verification:
    enabled: false
    backend: claude-code
    auto_reject_commit: true
  quality_gates:
    enabled: false
    test_command: ""
    timeout: 300
    block_on_regression: true
```

---

## New Packages

| Package | Purpose |
|---------|---------|
| `internal/memory` | Embedding, vector store, retrieval |
| `internal/decompose` | Task planning, DAG execution, verification, quality gates |

## Files Summary

| File | Phase | Purpose |
|------|-------|---------|
| `internal/memory/embeddings.go` | 1 | Embedding provider interface + Ollama/OpenAI impls |
| `internal/memory/store.go` | 1 | SQLite vector store with cosine similarity |
| `internal/memory/retriever.go` | 1 | Search + auto-save/retrieve hooks |
| `internal/memory/chunker.go` | 2 | Output chunking for granular search |
| `internal/decompose/planner.go` | 4 | LLM-based task decomposition |
| `internal/decompose/pipeline.go` | 4,5 | DAG executor + cycle detection |
| `internal/decompose/worker.go` | 4 | Subtask session management |
| `internal/decompose/verifier.go` | 6 | Independent verification agent |
| `internal/decompose/quality.go` | 7 | Test baseline + regression detection |
| `internal/config/config.go` | 1,6,7 | MemoryConfig, VerificationConfig, QualityGateConfig |
| `internal/router/router.go` | 1,2,3,4 | remember/recall/learnings/complex commands |
| `internal/server/web/app.js` | 4 | Pipeline view |
| `cmd/datawatch/main.go` | All | Wiring |
