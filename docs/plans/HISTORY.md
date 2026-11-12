# Shipped history

What datawatch has built, grouped by release line. Each entry is one BL item
and a one-line summary. For what changed in each release, and the exact
versions, see [CHANGELOG.md](../../CHANGELOG.md). Work that is still open is
tracked in [README.md](README.md).

Releases before v8.1.0 are not listed here. See the changelog.

## v9.1.x (current patch line)

- **BL447**: per-task project directory override, available on every surface (v9.1.0).
- **BL451**: a stale completion rollup can no longer overwrite a concurrent task reset (v9.1.1-v9.1.2).
- **BL454** (+ **BL495**): autonomous settings changed at runtime — the verification backend and model, the verification backends pool, the planner backend and model, the stall-detection limit and the verifier diff cap — now take effect on the very next task without a daemon restart (v9.5.30).
- **BL449**: 16-cell open-model test matrix runner, plus opencode model routing fixes (v9.1.3-v9.1.4).
- One-shot tasks no longer stall mid-task on the checkpoint protocol (v9.1.5).
- Compute nodes that represent the local machine attach cleanly to the local self-peer (v9.1.6-v9.1.7).
- Opencode panes show task text correctly, and finished work that was never committed is recovered (v9.1.8).

## v9.0.x (pre-v10.0.0 self-build arc)

### v9.0.0: Memory Lifecycle Complete

The memory trilogy closed, and the v8.x releases since were folded into this
major milestone.

- **BL385**: subprocess memory scope isolation, with session-local writes and scoped reads.
- **BL386**: memory lifecycle management, covering warm-start seeding, harvest on completion, archive on delete, and handoff.
- **BL387**: PRD memory integration, covering verifier memory, child inheritance, decomposer enrichment, cross-PRD seeding, and auto-report (shipped earlier, v8.31.0-v8.33.0 — see below).
- **BL389**: capacity-aware admission and queueing for Automata (three phases).

### v9.0.1-v9.0.7: release and restart fixes

- Container image builds were fixed after a Go version drift dropped most images from the release.
- `datawatch restart`, `stop` and `start` delegate to the systemd unit when one is active.

### Autonomous Automata: engine and guardrails (v9.0.10-v9.0.88)

- **BL404**: messaging channel configuration parity for the imap_mcp channel (v9.0.10, finished v9.0.28-v9.0.29).
- **BL406**: project-rules enforcement, with a scan engine, parity inheritance, multi-granularity rules, an upstream issue-filing action, and real guided mode (v9.0.11-v9.0.17).
- **BL407**: PRD git workflow, with one branch per PRD and an automatic PR on completion, including cluster mode (v9.0.18-v9.0.23).
- **BL403**: plugin extension surfaces, with manifest v3, supervised service plugins, the widget data model, and a generic PWA widget renderer (v9.0.31-v9.0.46). The federation capability gate for widgets has a completed PRD but no changelog entry yet.
- **BL416**: tooled, deterministic guardrails. A per-task verification script runs before any LLM judgment, and guardrail backend overrides and file provenance are visible on every surface (v9.0.34-v9.0.39). The documentation phase is deferred.
- **BL418**: PRD-completion rules review, an operator-configurable judgment check (v9.0.47-v9.0.48).
- **BL410**: failure classification, bounded auto-retry, content auto-fix, the learning writer, and a retry-policy CLI and comm surface (v9.0.50-v9.0.57).
- **BL419**: the kind-smoke CI job now runs real checks instead of passing silently (v9.0.59-v9.0.61).
- **BL421**: a blocked task can be recovered in place, with an editable spec (v9.0.62).
- **BL423**: a PRD waits for CI on the commit it pushed before it completes (v9.0.63).
- **BL426**: the agent-base container Trivy scan passes (v9.0.65).
- **BL429**: one-shot Claude Code tasks are delivered once, not re-sent on every heartbeat (v9.0.66).
- **BL430**: per-task, per-story and per-Automaton verification LLM override (v9.0.73). The PRD that originally carried this number for multi-Automaton orchestration was cancelled in favor of BL431.
- **BL425**: detection of a task whose commit swept in someone else's uncommitted work (v9.0.75).
- **BL432**: smaller payloads for mobile and PWA clients, with a compact session list, gzip for API JSON, paged alerts, WebSocket deltas, and subscribed-only output (v9.0.76-v9.0.82).
- **BL420**: session tracker repositories no longer grow until the daemon runs out of memory (v9.0.83-v9.0.84).
- **BL427** (phase 1): internal event dispatcher for Automaton lifecycle transitions (v9.0.85).
- **BL428** (phase 1): daemon-wide concurrency cap; the per-PRD parallel setting is deprecated (v9.0.86).
- **BL440**: the session Status tab shows Automaton task detail (v9.0.87).
- Fixed a clear error message for `llm_test` on session-backed kinds (v9.0.88).

## v8.x

### Early v8: community and PWA foundations (v8.1.0-v8.13.x)

- **BL324**: community skills and plugins registry on GitHub (v8.1.0).
- **BL325**: registry discovery UX and plugin install (v8.1.0).
- **BL326**: mic recording popup with animation and cancel and send controls (v8.1.0).
- **BL327**: settings badge multi-select UI (v8.2.0).
- **BL328**: async PRD decompose with SSE and polling (v8.2.0).
- **BL329**: identity endpoints (v8.2.0).
- **BL330**: UnifiedPush endpoints (v8.2.0).
- **BL331**: channel-address federation over comms (v8.3.0).
- **BL332**: discussion scopes for federated shared memory (v8.4.0).
- **BL333**: federated file service with PWA upload (v8.3.0).
- **BL318**: OpenCode provider API keys are kept out of plaintext config (v8.8.0).
- **BL336**: typing-aware send queue, so input does not clobber a live session (v8.9.18).
- **BL344-BL361**: session orchestration primitives (v8.10.3-v8.10.15). These cover alert click-through, lifecycle push events, a session tree view, lookup of your own session ID, orphan listing, recursive kill, federation lineage, recurring named schedules, name-addressed session operations, crash detection, exit hooks, a durable work queue, discussion subscriptions, restart from any state, a structured result store, and filtered session listing.
- **BL362**: MCP channel bridge diagnostics (v8.10.17).
- **BL319**: extra MCP servers injected into every spawned session (v8.13.0).
- **BL363**: goose backend testing coverage (v8.13.x-v8.14.0).
- **BL364**: OpenCode model picker sends the compute node, and the Ollama provider block is corrected (v8.13.34).

### Mid v8: autonomous PRDs mature (v8.15.0-v8.33.0)

- **BL368**: vision input across comms, skills, sessions and MCP (v8.15.0).
- **BL366**: the autonomous verifier grounds its verdict in the git diff (v8.16.0).
- **BL367**: quality gates for the autonomous executor (v8.17.0).
- **BL369**: prompt-injection hardening for the autonomous executor (v8.18.0).
- **BL371**: planning and execution backends split (v8.20.0).
- **BL370**: parallel fan-out for independent tasks, governed by `max_concurrent_tasks` (v8.26.0).
- **BL378**: GPU load for remote compute hosts (v8.26.0).
- **BL379**: per-system stats card on the Observer tab (v8.26.0).
- **BL380**: CPU, GPU and memory stats in the PRD status view (v8.26.0).
- **BL382**: cancel, requeue and resource bars for story and task lifecycle (v8.27.0).
- **BL381**: per-story LLM configuration with a PWA picker (v8.28.0-v8.28.2).
- **BL383**: session elapsed clock (v8.28.3).
- **BL384**: decomposer scope-drift hardening (v8.28.3).

### Memory and security (v8.33.0-v8.79.0)

- **BL387**: PRD memory integration, in three phases (v8.31.0-v8.33.0).
- **BL390** (phase 1): per-persona backend and model assignment for council mode (v8.38.0). Later phases are folded into BL405.
- **BL391**: multi-provider web search with usage tracking and caching (v8.39.0).
- **BL394**: review of Dependabot and code-scanning findings, mostly complete (v8.39.x).
- **BL396**: PWA parity sweep across the GitHub issue batch (v8.59.1).
- **BL397**: native ACME and Let's Encrypt support, with APNs push folded in as BL335 (v8.62.x). Delegated-subzone DNS-01 is deferred.
- **BL315**: PWA window expand and install prompt (v8.66.2).
- **BL317**: multi-server PWA with a server picker and aggregated views (v8.69.2).
- **BL399**: access and audit logging is complete across HTTP, WebSocket, auth failures and federation hops (v8.73.41-v8.74.0).
- **BL398** (initial phases): suppressed container-image CVE entries were triaged and documented (v8.73.x). The per-entry reachability review is still open.
- **BL401**: spawned sessions are steered toward datawatch's audited memory and discussion tools (v8.78.2).
