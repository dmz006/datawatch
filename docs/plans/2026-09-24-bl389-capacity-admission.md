# BL389 — Capacity-aware Automata admission and queueing

- **Date**: 2026-09-24
- **Version at planning**: v8.34.1
- **Status**: Implemented (all three phases) in the v9.0.0 working tree, awaiting release. Details of what shipped are in section 8.
- **Numbering note**: BL388 is reserved by the BL387 plan for "memory tagging / auto-promote / quotas (future)", so this item is BL389.
- **Raised by**: operator question, 2026-09-24: do PRDs observe each other and avoid starting sessions that cause hardware conflicts, and do they wait until compute has resources?

## 1. Problem (verified by code read, 2026-09-24)

Short answer to the operator's question: **no.** Each running PRD is an independent executor; nothing makes a PRD wait for capacity, and a capacity conflict becomes a task failure.

| Fact | Where |
|---|---|
| The only cross-PRD limit is the host-wide `session.max_sessions`. It counts running and waiting-input sessions on this host, including the operator's own interactive sessions. Default is 10 in the manager, 5 in the setup wizard. | `internal/session/manager.go:1536-1546`, `:476`; `internal/wizard/defs.go:907` |
| At the limit, `Start` hard-rejects with `max sessions (N) reached`. There is no queue and no wait. | `internal/session/manager.go:1545` |
| The executor treats that rejection as a failure: `executeOne` returns `spawn: ...` without retrying, the run loop marks the task failed, and dependents inherit the failure, so the PRD fails. | `internal/autonomous/executor.go` (spawn error return in `executeOne`; failure and dependency propagation in the run loop) |
| Task concurrency is per PRD (`max_concurrent_tasks`, default sequential). N running PRDs each fan out independently; there is no shared budget. | `internal/autonomous/executor.go:154-163`, `internal/autonomous/models.go:203` |
| Compute nodes carry `declared_capacity` (gpus, gpu_mem_gb, ram_gb, max_concurrent_models; default 1) and `scheduling_priority`. Both are stored, validated and shown in REST/PWA, but **nothing consults them when a session is spawned**. | `internal/compute/node.go:162-169, 365-369, 502-504`; consumers are config plumbing (`cmd/datawatch/main.go:166-169, 2933-2939`) and REST output (`internal/server/compute.go:333`) only |
| No per-node or per-LLM in-flight limit exists. | `internal/inference`, `internal/compute` (only the declared field) |
| The mitigations that exist are reactive: the SSE-stall watchdog kills a stalled opencode session and retries; chunk/header timeouts were raised to 20 and 15 minutes for slow cold model loads. These treat the symptom of overload, not the cause. | `cmd/datawatch/main.go` (`autonomousVerify` wait loop), opencode config generation |

To verify in Phase 1: whether planning (decompose) sessions go through the same `Start` path and cap (expected yes).

Practical effect on the operator's setup: one large model on one Ollama host (about 12 minutes to load) shared by every PRD, plus the operator's interactive sessions counted against the same cap. Two PRDs at once can stall each other or fail outright when the cap is reached.

## 2. Goals and non-goals

Goals:
1. A capacity conflict never fails a task. The task waits, visibly, and proceeds when a slot frees.
2. PRDs share capacity fairly; one busy PRD cannot starve another.
3. Declared node capacity is actually enforced.
4. The operator's interactive sessions are never queued or starved by autonomous work.
5. The operator can always see who holds capacity and who is waiting, on every surface.

Non-goals: cross-host scheduling (each daemon manages its own host; a federated view is a later item), GPU memory prediction beyond declared and observed values (Phase 3), pre-emption of running sessions.

## 3. Design

**3.1 Capacity ledger.** A small `internal/capacity` package holding named pools, each with a limit and a set of leases keyed by session id:
- `host`: session slots, from `session.max_sessions`.
- `node:<name>`: sessions allowed in flight on a compute node. New field `max_concurrent_sessions` on the node, alongside `declared_capacity`.
- `llm:<name>`: optional in-flight cap per LLM (useful for rate-limited hosted backends).

**3.2 Leases.** Acquired before `Manager.Start`, bound to the session id, released on any terminal state (complete, failed, killed, deleted) through the existing state-change hook. On boot the ledger is rebuilt from live sessions, so a restart cannot double-book. A periodic reaper reconciles leases against live sessions so a missed release cannot leak capacity (same class of bug as the session leak fixed in v8.34.0).

**3.3 Executor.** New task status `waiting_capacity`, set when a lease is unavailable, with the reason attached (which pool, which holders). Waiting is not a failure and does not consume `auto_fix_retries`. Waiting tasks sit in a fair queue: round-robin across PRDs, FIFO within a PRD, with an optional per-PRD `priority`. `capacity_wait_timeout` (default several hours) ends the wait with an error that names the blocking pool. Cancel and reset work while waiting; boot-resume re-enqueues waiting tasks. Planning (decompose) sessions use the same acquire path.

**3.4 Interactive headroom.** `session.reserved_interactive` slots are held back from autonomous work. Operator-started sessions are never queued.

**3.5 Model affinity and load awareness (Phase 3).** Prefer admitting the next task whose model is already loaded on the node, to avoid cold loads and eviction thrash. Use observer stats from `datawatch-stats` (GPU utilisation and memory against declared `gpu_mem_gb`) as a soft backpressure gate.

**3.6 Visibility and parity.** `GET /api/capacity` (pools, limits, holders, queue), MCP `capacity_status`, CLI `datawatch capacity`, a comm-channel command, a PWA panel plus a task badge such as "waiting for capacity: node datawatch 1/1, held by PRD ab12", Prometheus gauges for leases and queue depth per pool, and an alert when a wait exceeds a threshold. File the Android parity issue once the API shape is settled.

**3.7 Configuration.** `autonomous.capacity.enabled` (default on in v9.0.0), `autonomous.capacity.wait_timeout`, `session.reserved_interactive`, per-node `max_concurrent_sessions`, per-LLM `max_inflight`, per-PRD `priority`.

## 4. Phases

- **Phase 1 (small, ships alone):** treat `max sessions reached` as wait-and-retry with backoff instead of a task failure; add the `waiting_capacity` status and reason; waiting does not count as a retry; bounded by a timeout. Verify the decompose path. This alone removes "capacity conflict fails the PRD".
- **Phase 2:** the ledger, node and LLM pools, cross-PRD fair queue, reserved interactive slots, and the API/MCP/CLI/comm/PWA surfaces, metrics and docs.
- **Phase 3:** model affinity and GPU-aware backpressure from observer data.

## 5. Tests, docs and e2e (standing requirement: 1:1 with each change)

- **Unit:** ledger acquire/release/limits, fairness across PRDs, boot rebuild, leak reaper, race tests under `-race`; executor: waiting does not consume retries, cancel-while-waiting, timeout, restart with queued tasks; handler tests for API, MCP, CLI and comm.
- **Smoke:** two PRDs against a node with capacity 1: the second waits, then proceeds after the first finishes.
- **E2E:** new story running two concurrent PRDs against a capacity-1 node, asserting states through the API and zero failed tasks; extend the dual-node story so it also asserts distribution across nodes.
- **Docs:** new howto `automata-capacity.md` (no backlog IDs in user-facing text), pitfalls entry in `autonomous-planning.md`, `config-reference.yaml`, definitions, `openapi.yaml`, `api-mcp-mapping.md`, and a flow diagram under `docs/flow/`.

## 6. Risks and open questions

- Default for per-node `max_concurrent_sessions` (1 matches a single large model on one GPU but serialises every PRD on that node). Turning admission on by default is a behaviour change, so it goes in the v9.0.0 upgrade notes.
- Whether operator interactive sessions on the same node count against the node pool, or only against the host pool.
- How to size model memory: declared, observed, or a per-model hint.
- Behaviour when a node is unreachable: fail fast versus wait.
- Federation: per-daemon pools now, aggregated view later.

## 7. Definition of done

Two PRDs on one capacity-1 node never fail for capacity and never run more than one session on it; the operator can see the holder and the queue on every surface; interactive sessions are unaffected; all tests above pass; docs and parity issue filed.

## 8. What was built

- `internal/capacity`: the ledger (pools, limits, all-or-nothing leases, fair queue with priority, aged waiters, model affinity, external-usage counting, backpressure hook, reaper, snapshot).
- Executor (`internal/autonomous/capacity.go`, `executor.go`): admission before each spawn; `waiting_capacity` status with `wait_reason`; a full `session.max_sessions` is a wait-and-retry, not a failure; waiting consumes no auto-fix retry; cancel removes a waiter; boot-resume re-queues waiting tasks; leases released when the task finishes. Planning (decompose) sessions retry on a full session cap.
- Wiring (`cmd/datawatch/capacity_wiring.go`): 10 s sync of limits from `session.max_sessions` minus `session.reserved_interactive`, compute node `max_concurrent_sessions`, LLM `max_inflight`; GPU backpressure from node stats when `autonomous.capacity_gpu_util_pct` is set; leaked-lease reaper; 30-minute wait alert; Prometheus gauges.
- Surfaces: `GET /api/capacity`, `POST /api/autonomous/prds/{id}/set_priority`, MCP `capacity_status` and `autonomous_prd_set_priority`, node/LLM update params, CLI `datawatch capacity`, comm `capacity`, PWA Capacity card and task badge and settings.
- Decisions taken: per-node and per-LLM limits default to unlimited (opt-in); `session.reserved_interactive` defaults to 1 when `max_sessions >= 3`; operator sessions count against the host pool only; GPU backpressure defaults off; wait timeout 4 hours; per-daemon pools only (federated view is later).

## Parity surface

| Surface | Status |
|---|---|
| REST | `GET /api/capacity`, `POST .../set_priority`, config keys on `/api/config` |
| MCP | `capacity_status`, `autonomous_prd_set_priority`, node/LLM update params |
| CLI | `datawatch capacity`; `datawatch config set` for the keys |
| comm channel | `capacity`, `configure` for the keys |
| YAML/config | `autonomous.capacity_*`, `session.reserved_interactive`, node `max_concurrent_sessions`, LLM `max_inflight` |
| PWA | Capacity card, waiting badge, priority and limit fields |
| Android | Not in this change; issue filed in datawatch-app (show waiting_capacity, capacity view, priority) |
| iPhone | Not in this change; issue filed in the iOS repo when it accepts parity issues (capability parity, idiomatic delivery) |
