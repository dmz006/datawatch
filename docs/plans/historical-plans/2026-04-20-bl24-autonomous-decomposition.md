# BL24 — Autonomous task decomposition (+ BL25 verification)

**Status:** v1 design, implementation underway in v3.10.0 (Sprint S6).
Operator directive 2026-04-19: improve on nightwire using datawatch
primitives.

---

## 1. What we're building

A four-tier work breakdown — **PRD → Stories → Tasks → DAG** — driven
autonomously by an LLM, with each task spawned as a worker session
under datawatch's existing F10 spawn primitives. Each task ends with
an independent verification step (BL25) before the parent considers
it done.

End-state goal: operator types one feature description, datawatch
splits it into a story DAG, spawns workers (parallel where possible),
runs quality gates, verifies output with a fresh agent, and surfaces
the rolled-up status back to the operator.

---

## 2. What nightwire ships (and what's worth re-using)

`HackingDave/nightwire` (Python, MIT-licensed; 156★) ships an
autonomous system in `nightwire/autonomous/` with these components:

| File | What it does | Re-use approach |
|---|---|---|
| `models.py` | PRD / Story / Task / Learning Pydantic models with status enums | **Re-derive in Go** as `internal/autonomous/models.go`. Nightwire's status taxonomy (PENDING / QUEUED / IN_PROGRESS / RUNNING_TESTS / VERIFYING / COMPLETED / FAILED / BLOCKED / CANCELLED) maps cleanly. |
| `database.py` | SQLite persistence for PRDs/Stories/Tasks/Learnings | **Re-use the schema**, but persist as JSON-lines under `<data_dir>/autonomous/*.jsonl` initially — datawatch's `store` pattern. SQLite upgrade later via `internal/memory` if needed. |
| `manager.py` | Central coordinator — boots executor + loop + quality gates | Map to **`autonomous.Manager`**, wired in main.go alongside `pipeline.Executor`. |
| `executor.py` | Runs individual tasks: pre-task test baseline, fail-closed verification, auto-fix retry, git checkpoints | Map to **`autonomous.TaskRunner`** that delegates to: `pipeline.Executor` for the DAG, `session.Manager.Start()` for tmux sessions, `pipeline.QualityGateConfig` (BL28) for baseline tests, `ProjectGit.TagCheckpoint` (BL29) for pre/post tags, and the verifier (BL25) for fail-closed checks. **The auto-fix retry loop is the meaningful add over nightwire** — re-prompt the same backend with the verifier's diff. |
| `verifier.py` | Independent verification — fresh Claude context, diff-based, severity levels, caching | Map to **`autonomous.Verifier`** that spawns a `BL103 validator agent` worker (or a fresh `session.Manager.Start()` with a different `Backend`/`Profile` for cross-backend independence) and reads the output. Diff-based cache keyed on `git rev-parse HEAD` after the task. |
| `loop.py` | Background async loop, parallel workers, stale-task recovery (60-min stale timeout) | Map to **`autonomous.Loop`** — Go goroutine, `select`+ticker, max-parallel cap. **Reuse BL40** (`session.IsStale` + `ListStale`) for the stale-task path instead of nightwire's hard-coded constant. |
| `quality_gates.py` | Test / typecheck / lint timeouts + security pattern scanner | **Reuse BL28** (`pipeline.QualityGateConfig` + `RunTests`) for the test-baseline path. Port nightwire's security scanner as `autonomous.SecurityScan(projectDir)` — useful even outside autonomous mode. |
| `learnings.py` | Extracts post-task learnings | Bridge to **datawatch memory** (`memory_remember` + KG via `BL57` knowledge graph). No re-derivation needed — datawatch's memory layer is more mature. |
| `prd_builder.py` | LLM-output → JSON parsing with smart-quote / comment / fence cleanup | **Port the cleanup heuristics** to Go (`internal/autonomous/prd_parse.go`) — these are battle-tested for LLM JSON output. |

---

## 3. What datawatch contributes that nightwire doesn't have

This is where v3.10.0 BL24 should be measurably better than nightwire:

| Datawatch primitive | Why it matters for autonomous decomposition |
|---|---|
| **F10 ephemeral container-spawned agents** (Docker + K8s drivers) | Each story can spawn a clean container per task — nightwire uses local workspace + git locks. Containers eliminate the cross-task contamination class of bug entirely. |
| **F15 pipeline DAG executor** with cycle detection (BL39) and quality gates (BL28) | The Story → Tasks tree is a DAG; we already have `pipeline.NewPipeline(name, dir, tasks, maxParallel)` + cycle reject + per-task pre/post test runs. Don't re-derive. |
| **BL96 wake-up stack (L0–L4)** with per-agent overlay | Sub-agents inherit parent context AND see siblings — nightwire's verifier has no parent context awareness, so it re-discovers facts every call. We can prepend the parent PRD to the verifier's L0 wake-up prompt for free. |
| **BL103 validator agent image** (read-only post-session attestor, distroless ~5MB) | Already exists. The verifier becomes "spawn one of these per task"; no new image needed. Cross-backend independence is automatic via the validator image's pinned model. |
| **BL10/BL11/BL12 observability** (diff capture, anomaly detection, day-bucket analytics) | Per-task progress visibility comes free — every spawned worker is a `session.Session` already, so diff/anomaly/cost/audit work without changes. |
| **BL104 peer broker** (worker P2P messaging) | Workers in parallel can publish progress to siblings via the existing peer broker — nightwire serializes through the manager. |
| **BL30 cooldown + BL40 stale recovery** | Already operator-tunable. Autonomous loop respects existing knobs instead of hard-coding 60-min stale timeout. |
| **BL6 cost tracking** | Per-PRD cost rollup is automatic — sessions already report `tokens_in/out/est_cost_usd`, autonomous just sums by task → story → PRD. |
| **BL9 audit log** | Every autonomous action is recorded as an audit entry (actor=`autonomous`) — no new log infrastructure. |

---

## 4. v1 scope (what ships in v3.10.0)

Strict cuts to keep v1 tractable:

**In:**
- Data models (PRD, Story, Task, Learning, Status enums) in `internal/autonomous/`.
- JSON-lines persistence under `<data_dir>/autonomous/{prds,stories,tasks,learnings}.jsonl`.
- `Manager` API: `CreatePRD(spec)`, `Decompose(prd, llmFn)`, `Run(prd, ctx)`, `Status(prdID)`, `Cancel(prdID)`.
- `Executor` that wraps `pipeline.Executor` for the Task DAG and reuses BL28 quality gates + BL29 checkpoints.
- `Verifier` that posts the post-task git diff to the BL103 validator agent (or to a `session.Manager.Start()` with a different profile when no validator is configured).
- `Loop` background goroutine — uses BL40 `session.ListStale` for crash recovery.
- LLM decomposition prompt (claude-code by default; configurable per `session.routing_rules`).
- REST endpoints + full parity (MCP + CLI + comm).
- Tests + per-component docs.
- Operator usage doc at `docs/api/autonomous.md` covering the LLM prompt template.

**Out (deferred to v3.10.x patches or BL117):**
- Web UI (Gantt-style story board) — REST works for now; a wrapping UI is its own feature.
- Cross-cluster orchestration — single-cluster only at v1.
- Plugin/external-tool integration — datawatch already has `BL103 validator`; plugin framework is BL33 (sprint S7).
- Auto-pause + auto-resume across daemon restarts — uses the existing BL93 reconciler at v1; richer resume semantics later.
- `prd_parse.go` smart-quote/fence cleanup ports as a small helper now; the rest of nightwire's heuristics are deferred.

---

## 5. New rules / config

Per the no-hard-coded-config rule, every knob below is reachable from
YAML + REST + reload + MCP + CLI + comm:

```yaml
autonomous:
  enabled:                false          # off by default; opt-in
  poll_interval_seconds:  30             # background loop tick
  max_parallel_tasks:     3              # in-flight worker cap (per-PRD)
  decomposition_backend:  ""             # empty = use session.llm_backend
  verification_backend:   ""             # empty = use session.llm_backend (BL25)
  decomposition_effort:   "thorough"     # BL41 effort hint
  verification_effort:    "normal"
  stale_task_seconds:     0              # 0 = use session.stale_timeout_seconds
  auto_fix_retries:       1              # how many times to re-prompt on verification failure
  security_scan:          true           # apply nightwire-port scanner before commit
```

REST surface:
```
POST   /api/autonomous/prds                  body: {spec, project_dir, [backend], [effort]}
GET    /api/autonomous/prds                  list all
GET    /api/autonomous/prds/{id}             fetch one (with story tree + task status)
DELETE /api/autonomous/prds/{id}             cancel + archive
POST   /api/autonomous/prds/{id}/run         kick the loop (or restart on crash)
POST   /api/autonomous/prds/{id}/decompose   re-run the decomposition LLM call
GET    /api/autonomous/learnings             extracted learnings (paginated)
```

---

## 6. Open questions for the operator

These don't block v1 — listed so the next iteration can settle them
explicitly:

1. **Verifier independence model.** v1 spawns the BL103 validator
   image when configured, falling back to a fresh `session.Manager.Start()`
   with whichever backend is set. Should we *require* a different
   backend (cross-backend independence guarantee) or allow same-backend
   verification with an explicit flag?
2. **Auto-fix retry depth.** Default 1. Higher → more autonomy
   (good); higher → more spend (bad). Per-PRD override?
3. **PRD storage.** JSON-lines now; SQLite (via `internal/memory`)
   later. Want the upgrade in v3.10.x or wait for usage signal?
4. **Cost cap per PRD.** Should `autonomous.max_usd_per_prd` exist as
   a circuit breaker that pauses the loop when exceeded?

---

## 7. Non-goals (explicitly)

- This is **not** a replacement for `pipeline.Pipeline` — pipelines
  are operator-defined DAGs, autonomous is LLM-derived.
- This is **not** a replacement for `agents.Orchestrator` (BL105) —
  orchestrator is multi-container DAG execution, autonomous can
  *call* into it.
- This is **not** the BL117 PRD-DAG orchestrator — BL117 adds
  guardrail sub-agents (rules / security / release-readiness /
  docs-diagrams-architecture). Autonomous v1 is the substrate.
