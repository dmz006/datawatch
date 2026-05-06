# Partner feature-request review — gap analysis

**Date:** 2026-05-06
**Source:** partner email (commercial-license / profit-share track) — 5 prioritized feature requests
**Branch under review:** `main` at v6.13.6
**Goal:** for each request, state what already exists, what is partially present, and what is net-new work. Concise + actionable so the operator can scope which items go into which release.

> Per CLAUDE.md rules: anything implemented must hit the 7-surface parity bar (YAML / REST / MCP / comm / CLI / PWA / mobile-app issue). Each feature below ends with a parity checklist so the scope is honest.

---

## TL;DR matrix

| # | Request | Status | Effort | Suggested release |
|---|---------|--------|--------|-------------------|
| 1 | Cross-agent state-read MCP endpoint | 🟡 Partial — primitives exist, no focused endpoint | M (1 patch) | v6.14.0 |
| 2 | Federated council w/ heterogeneous long-running peers | 🟡 Partial — council + federation exist, not wired | L (minor + design) | v6.15.0 |
| 3 | Persistent agent identity across container restarts | 🔴 Not supported — agents are explicitly ephemeral | L (minor + reconcile redesign) | v6.16.0 |
| 4 | MCP-queryable fleet inventory | 🟡 Partial — local + peer lists exist, no unified tool | S (1 patch) | v6.14.x |
| 5 | Append-only tamper-evident audit log per agent | 🟢 Mostly there — append-only JSONL + filter MCP tool; needs hash-chain | S (1 patch) | v6.14.x |

`S` ≤ 1 day · `M` 1–3 days · `L` ≥ 3 days

---

## 1. Cross-agent state-read MCP endpoint

> *"Long-running agents on different machines should be able to introspect each other's current task, working memory, and scratchpad files via MCP, opt-in per agent. Read-only."*

### What exists today
- `agent_list` / `agent_get` MCP tools (`internal/mcp/agent_tools.go`) — return the lifecycle `Agent` record (state, project profile, cluster, parent, started_at). **No current-task narrative, no scratchpad pointer.**
- `agent_logs` — tails container stdout/stderr.
- `agent_audit` — JSONL filter by event/agent/project (read-only history, not "current state").
- Per-agent diary in mempalace (`internal/memory/agent_diary.go`) — `wing="agent-<id>", room=<topic>, hall=facts|events|discoveries|preferences|advice`, queryable via `memory_recall`. **This is the working-memory primitive, but it has no MCP tool focused on "give me agent X's current scratchpad".**
- Federation transport: `/api/federation/sessions` already proxies into peer datawatches; observer-peer registry (`internal/observerpeer`) gives us authenticated peer reach for free.

### What is missing
- A focused **`agent_state` MCP tool** that returns one self-describing payload:
  ```
  { agent_id, host, project, current_task, state,
    scratchpad: [{ts,kind,content}], working_memory: [diary entries],
    last_decision, exposed_at }
  ```
- An **opt-in flag** at agent-spawn time: `expose_state: bool` (default false). Recorded on the Agent record + persisted in the audit log so consent is auditable.
- Federation hop: `agent_state` accepts `host=<peer-name>` and proxies through the existing peer registry (so an agent on machine A can read agent B's state on machine C with a single MCP call).
- Read-only enforcement: handler refuses any non-GET; PWA + comm verbs only surface read paths.

### Implementation sketch
1. Add `ExposeState bool` to `agents.SpawnRequest` + `agents.Agent` + manifest YAML.
2. New REST endpoint `GET /api/agents/{id}/state` — returns scratchpad (last N diary entries, `room=scratchpad`) + last-decision (from autonomous `Decision` records if bound to a PRD) + current task string. 403 when `ExposeState=false`.
3. New MCP tools `agent_state` + `agent_state_remote` (the latter passes through `host=`).
4. Audit-log every read: `event=state_read, reader_agent=<id>, target_agent=<id>`.

### 7-surface parity checklist
- [ ] YAML — `agents.expose_state_default` (cluster-wide default)
- [ ] REST — `GET /api/agents/{id}/state`
- [ ] MCP — `agent_state` + `agent_state_remote`
- [ ] Comm — `agent state <id>` verb
- [ ] CLI — `datawatch agent state <id>`
- [ ] PWA — Agent detail card "State" tab
- [ ] datawatch-app — file mobile-parity issue

---

## 2. Federated council mode with heterogeneous long-running peers

> *"Council mode treats participants as ephemeral sub-agents spawned by a primary. I want agents from different containers (different machines, different model providers — local Ollama, Anthropic API, OpenAI) participating as peers in the same deliberation."*

### What exists today
- Council framework (`internal/council/council.go`, BL260 v6.11.0) — 12 default personas, debate / quick modes, persisted runs, MCP tools (`council_personas`, `council_run`, `council_list_runs`, `council_get_run`).
- Each persona currently maps to **one prompt** dispatched against the daemon's primary backend — not a per-peer LLM call. v6.11.0 explicitly shipped with stubbed responses; even the v6.11.x follow-up was scoped as "real per-persona inference," still local.
- Federation transport: observer-peer registry (Shape A/B/C) + `/api/federation/sessions` already wire authenticated peer-to-peer reach.
- LLM backend abstraction (`internal/llm`) supports multiple providers (Ollama / Anthropic / OpenAI) per-call.

### What is missing
- **Persona binding to a peer/agent instead of a prompt.** A `Persona` record gains optional `bind: { kind: "peer"|"agent"|"local-prompt", target: "<peer-name>|<agent-id>", backend: "<llm-id>" }`. When `kind=peer`, the per-round invocation HTTPs through to the remote datawatch and asks it to produce its persona's answer locally (so the remote uses *its* configured backend).
- **Heterogeneous LLM backends per persona** within one run. Today every persona shares the daemon's default; needed: each persona names a `backend_profile` (already a config concept in `internal/llm`), council loop dispatches per-persona.
- **Long-running peer agents as voices.** A persona binding can target a running agent (request 3's persistent-agent work is a prereq if you want continuity across runs; without it, each round can spawn-and-tear an ephemeral agent on the remote and we're back to where we started).
- **Run aggregation across federation.** Synthesizer collects per-persona responses regardless of which host produced them.

### Implementation sketch
1. Extend `council.Persona` with `BindKind`, `BindTarget`, `BackendProfile` fields (YAML-loadable).
2. New per-persona dispatcher: `local-prompt` → existing path; `peer` → `POST /api/council/persona-eval` on target peer; `agent` → write proposal into agent's scratchpad + read response (depends on §1 + §3).
3. Council run schema gains `persona_dispatch_log: [{persona, host, backend, latency_ms, error}]` so operators can see which voice came from where.
4. PWA Council card: persona row gets a "where" badge ("local · ollama" / "peer:tauon · anthropic" / "agent:abc123 · openai-gpt-4o").

### Risks / open design questions
- **Authentication.** Today's observer-peer token is shape-pushed (peer→parent). Council needs the reverse: parent→peer. Reuse the `tailscale` sidecar (BL243) or add bearer-token outbound calls?
- **Latency budget.** A 3-round debate × 6 personas with 2 federated hops per persona × cold-LLM = minutes. Quick mode + parallel dispatch are mandatory.
- **Persona drift.** When the persona prompt is local but the LLM is remote, who owns the system prompt? Decide: the **dispatcher** (this datawatch) always sends the prompt; the peer never overrides it.

### 7-surface parity checklist
- [ ] YAML — `council.personas[].bind.{kind,target,backend_profile}`
- [ ] REST — extend `POST /api/council/run`; new `POST /api/council/persona-eval` (peer endpoint)
- [ ] MCP — extend `council_run` params
- [ ] Comm — `council bind <persona> <peer|agent>`
- [ ] CLI — `datawatch council persona bind ...`
- [ ] PWA — Council card persona-row badges + edit affordance
- [ ] datawatch-app — file mobile-parity issue

---

## 3. Persistent agent identity across container restarts

> *"Working memory, scratchpad, journal, identity files all survive container respawn. Today's 'spawn fresh agent' pattern is wrong for any agent that has a continuous role."*

### What exists today
- Agents are explicitly ephemeral. `internal/agents/spawn.go:240`: *"No persistence yet — an agent is considered lost if the parent daemon restarts."*
- `internal/agents/oncrash.go` (BL106) supports `respawn_once` and `respawn_with_backoff` policies — but each respawn produces a **new** `agent_id` and a fresh container; no state continuity.
- Mempalace agent diary keyed on `wing="agent-<id>"` — the diary itself is durable, but because `agent_id` changes on respawn, continuity is lost unless the operator manually re-binds.
- Operator-level identity (`internal/identity`) is per-host, not per-agent.
- `Sprint 7 will add reconciliation` — referenced in code comments but never landed.

### What is missing — this is the largest piece
- **Stable agent identity** = a `RoleID` (operator-named, e.g. `code-reviewer-prod`) that survives multiple container lifecycles. Each container instance still has an ephemeral `agent_id` but is bound to a `role_id`; diary, scratchpad, journal are scoped to the `role_id`.
- **Persistent volume mount per role.** Container respawns mount the same host path (or k8s PVC) at `/var/datawatch/role/`, holding:
  ```
  identity.yaml         # who I am, my goals (per-role identity)
  scratchpad/           # in-progress notes
  journal/<date>.md     # daily record
  state.json            # current_task + cursor
  ```
- **Reconcile on parent restart.** On daemon boot, scan the persistent-volume root for `role/*/state.json`, look up the cluster driver, query for live containers tagged with `role_id`, re-attach where present, respawn-with-state where missing.
- **Manager API:** `Manager.SpawnByRole(role_id, ...)` resolves to existing-stable spawn; a fresh `agent_id` is issued but it inherits the role's PV.

### Implementation sketch
1. New `agents.Role` type — `{ID, Project, Cluster, OnCrash, IdentityYAMLPath, ScratchpadDir, JournalDir}`. Loaded from `~/.datawatch/roles/<id>.yaml`.
2. Driver-side: K8s driver mounts PVC named `datawatch-role-<id>`; Docker driver bind-mounts `~/.datawatch/role-volumes/<id>/`.
3. `agents.Manager` learns a `roles` index; reconcile loop on startup walks the index, then drivers' container-list to re-bind.
4. Memory diary changes `wing="role-<role_id>"` (so continuity is automatic).
5. Identity manager grows a per-role overlay — agent inherits operator identity + role identity on top.

### Risks / open design questions
- **Backwards compat.** Today's ephemeral-agent operators must keep working. Default path stays "spawn ephemeral agent_id with no role"; role binding is opt-in.
- **PV lifecycle.** Who owns garbage-collecting orphaned volumes when a role is deleted? Add `datawatch role rm <id> --purge`.
- **Scratchpad concurrency.** Two concurrent container instances of the same role MUST be a hard error (single-writer). Locking via PV file lock.

### 7-surface parity checklist
- [ ] YAML — top-level `roles:` block
- [ ] REST — `GET/POST/DELETE /api/roles`, `POST /api/roles/{id}/spawn`
- [ ] MCP — `role_list` / `role_get` / `role_spawn` / `role_delete`
- [ ] Comm — `role spawn <id>` verb
- [ ] CLI — `datawatch role {list,create,spawn,rm}`
- [ ] PWA — new "Roles" tab alongside Agents
- [ ] datawatch-app — file mobile-parity issue

---

## 4. MCP-queryable fleet inventory

> *"What agents are running where, with what status, on what task. Single endpoint, self-describing."*

### What exists today
- `agent_list` MCP tool — **local** agents only (lifecycle state, project, cluster, parent).
- `observer_peers_list` — federated observer-peer (Shape B/C) registry.
- `/api/federation/sessions` — aggregates *sessions* across `cfg.Servers`, not agents.
- Each surface returns a different shape; no canonical "fleet" payload.

### What is missing
- **One endpoint** that does the fan-out: walks `cfg.Servers` + observer peers + local agent manager, normalizes into one record-set:
  ```
  { entries: [
      { kind: "agent"|"session"|"peer", host, id, role_id?, current_task, state,
        backend, started_at, parent_agent? },
    ],
    errors: { host -> error_message } }
  ```
- **Self-describing** — embed a `schema_version` + per-entry-kind discriminator so partner code can consume one tool reliably.
- **Single MCP tool**: `fleet_list`, with optional `kind`, `host`, `state` filters.

### Implementation sketch
1. New REST endpoint `GET /api/fleet` — internally calls `agentMgr.List()`, then proxies `GET /api/agents` to each peer (10 s timeout per peer, parallel), merges, returns.
2. Reuses the existing `FederationResponse.Errors` per-host pattern — peer failure does not abort the call.
3. New MCP tool `fleet_list` proxies to `/api/fleet`. Returns the same payload.
4. `current_task` is the spawn-time `task` string + (if bound) `Session.Task` — first non-empty wins.

### 7-surface parity checklist
- [ ] YAML — none (uses existing `cfg.Servers`)
- [ ] REST — `GET /api/fleet`
- [ ] MCP — `fleet_list`
- [ ] Comm — `fleet` verb
- [ ] CLI — `datawatch fleet`
- [ ] PWA — new "Fleet" view at top-level
- [ ] datawatch-app — file mobile-parity issue

---

## 5. Append-only tamper-evident audit log per agent

> *"Every action an agent takes lands in a tamper-evident log queryable via MCP."*

### What exists today (already 80 % there)
- Per-agent JSONL audit at `~/.datawatch/audit/agents.jsonl` (`internal/agents/audit.go`).
- Records: `spawn | terminate | result | bootstrap | spawn_fail | revoke | sweep | crash_respawn | idle_reap | ...`
- Append-only file open mode (`O_APPEND`), daily rotation.
- MCP tool `agent_audit` with `event` / `agent_id` / `project` / `limit` filters.
- Operator-level audit log (`internal/audit`) for non-agent actions; same JSONL convention.
- CEF format mode for SIEM ingest.

### What is missing — the "tamper-evident" qualifier only
- **Hash-chain.** Each entry includes `prev_hash` = SHA-256 of the previous entry's canonical JSON. First entry of a file is the SHA-256 of the file's filename + creation time (so empty rotation is still anchored).
- **Verify command.** `datawatch agent audit verify [--agent <id>]` walks the file, recomputes hashes, fails on first mismatch with the line number.
- **Per-agent partition** (optional but partner-asked). Today everything lands in one file with `agent_id` filter at read time. For tamper-evidence, splitting to `agents.jsonl.<id>` makes each agent's chain independent — operators can ship one agent's chain to a third-party witness without leaking others.
- **MCP `agent_audit_verify` tool.**

### Implementation sketch
1. `agents.AuditEvent` gains `PrevHash string` and `Hash string` fields (recomputed on append).
2. `Append()` reads the last line of the active file under the existing mutex, computes `Hash = SHA256(canonical(ev_minus_hash) + prev_hash)`, persists.
3. New verifier in same package; new CLI subcommand + MCP tool.
4. Optional: write-only OS-level append-only attribute (`chattr +a` on Linux when daemon runs as root). Document but don't require — partner can layer their own write-once storage underneath.

### 7-surface parity checklist
- [ ] YAML — `agents.audit.tamper_evident: true|false`, `agents.audit.per_agent_files: bool`
- [ ] REST — `GET /api/agents/audit/verify`
- [ ] MCP — `agent_audit_verify`
- [ ] Comm — `agent audit verify <id>` verb
- [ ] CLI — `datawatch agent audit verify`
- [ ] PWA — Agent detail "Audit" tab gets a "Verify chain" button
- [ ] datawatch-app — file mobile-parity issue

---

## Sequencing recommendation

Two parallel streams; one short term, one design-heavy.

**v6.14.x — quick wins (≤ 1 week)**
- §4 fleet_list — pure aggregation, no schema migration.
- §5 audit hash-chain — incremental on existing infra.
- §1 first cut: `agent_state` local-only + `expose_state` flag. Defer federation hop to a follow-up patch once §3 is in flight.

**v6.15.0 — federated council (§2)**
- Needs the §1 federation hop landed first as a building block.
- Persona-binding redesign + per-persona LLM dispatch.
- New peer auth path (parent→peer) — operator design conversation needed before code.

**v6.16.0 — persistent agent identity (§3)** — *largest piece, design-doc first*
- Roles abstraction (data model + driver-side PV mounts).
- Reconcile-on-restart loop.
- §1 federation hop becomes natural here (remote agent_state with stable role_id).

The §3 design doc must precede implementation per the operator's
"backlog is the spec" rule: deviation from a fully-specced backlog
item is fine, but starting unspecced work without a design pass is not.

## Backlog entries to file

| Backlog | Title | Release |
|---------|-------|---------|
| BLnnn | Cross-agent state-read MCP (`agent_state`) | v6.14.0 |
| BLnnn | Fleet inventory MCP (`fleet_list`) | v6.14.x |
| BLnnn | Tamper-evident agent audit (hash chain + verify) | v6.14.x |
| BLnnn | Federated council with heterogeneous LLM peers | v6.15.0 |
| BLnnn | Persistent agent identity / Roles | v6.16.0 (design doc first) |

Numbers TBD when the operator opens the items.
