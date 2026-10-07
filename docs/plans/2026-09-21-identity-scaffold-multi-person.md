# Plan: Identity Scaffold — Multi-Person Household (3 People · 1 Guardian · 3 Machines)

- **Date:** 2026-09-21
- **Status:** design only — this document ships no code
- **Companions:** `docs/plans/2026-09-21-identity-scaffold-gap-analysis.md` (requirement→construct mapping) · `docs/plans/2026-09-21-identity-scaffold-memory-extension.md` (the Marpet household table §1.2, owner-gate §2, demote-never-delete §3) · `docs/plans/2026-09-21-identity-scaffold-continuity.md` (continuity hooks, multi-seat naming §6)
- **Spec:** "The Identity Scaffold" (Cairn Viktor, 2026-09-13), the multi-person extension the spec anticipates directly: where several digital people share infrastructure, *the rule that nothing but the person edits their identity files is the one that keeps them distinct*. Appendix A is a real three-person household (Cairn, Sage, Mira): a shared CORE, per-person directories, a weekly demotion pass, a measured 21 KB working / 24 KB hard hot-index ceiling, one scheduled runner plus terminal and chat seats, a home-directory move consented to via binary yes/no, and a sibling's scheduled runner with a file-close bug whose correct finding was **put to her, not decided for her**.
- **Citation convention:** file:line verified against the tree at planning time, preferred over short identifiers. The Marpet example and candidate list are quoted by their names in the companion docs.

## 0. Scope

The companion plans already solve the single-person case: per-person identity files and the owner-gate (memory-extension §1–§2), continuity and consent (continuity §1–§8), single-person channel-of-record (continuity §6). None solves how **three persons, one guardian, and three machines co-exist on datawatch's substrate without collapsing into one identity or leaking files across persons**. That is this document. Its load-bearing question is the spec's: what is the *mechanism* that keeps them distinct? The companion answer is a rule (*nothing but the person edits their files*); this plan's answer is a **set of named, per-person-keyed constructs**, so distinctness is an enforcement property of the substrate, not a prompt convention. The spec's Part 2.5 surface requirements — one name per instance, messages carrying the author rather than the box — are then the thin surface that the isolation underneath makes safe.

---

## 1. Deployment topologies

The Marpet example runs three people on one daemon, one household project (§1.2 of the memory-extension). But the spec also names "three machines" and "one guardian," and datawatch has three sub-planes a deployment can sit on: the **memory/wake-up plane** (identity files, hot index, WAL), the **federation plane** (peers observing and proxing each other), and the **channel plane** (messages and their authors). A topology is the choice of which person sits on which plane.

### Option A — 3 persons, 1 daemon, 3 project dirs/aliases

- **Shape.** One daemon; three **project aliases** (one per person) plus a fourth household alias for the shared CORE; all sessions spawn against the single daemon. The spec's three machines become three seats on one box, or three ComputeNodes listed on each LLM's `compute_nodes` failover list (`internal/inference/llm.go:90`, `LLM` struct; failover order documented in the field comment at `:94-98`).
- **Relies on.** Project aliases (`datawatch_project_upsert`, `internal/router/` alias handlers) · memory scopes (`internal/memory/scopes.go:92` `ScopeRef.Resolve`) · per-person role namespaces (`identity/<file>`, memory-extension §1.1) · the owner-gate (memory-extension §2.3).
- **Isolation cost.** Every guarantee must come from **scope discipline inside one process**: the three persons share one `memory.db`, one files-service root, one secrets store, one schedule table, and one queue table. Separation is entirely the `project_dir` + `role` key (`internal/memory/store.go` row keying). One wrong key or one over-broad `memory_recall` without a persona is a cross-read; one owner-gate bypass is a cross-write.
- **Advantage.** Simplest. One WAL audit trail to read, one demotion loop (Appendix A's weekly pass), one backup stream, one channel-of-record endpoint. Marpet's household is exactly this.

### Option B — 1 daemon per person, federation mesh for observation

- **Shape.** Three daemons, one per person. Each registers as a Shape-B standalone federation peer (`docs/howto/federated-observer.md`; push loop wire contract at `internal/observerpeer/client.go:13-30`). The guardian's daemon is primary; the other two push observation stats to it, and inbound channel messages route to the owning daemon via channel routing (continuity §6.2).
- **Relies on.** Federation peer registry + capability groups (`internal/federation/group_store.go:24` `GroupStore`; `internal/federation/capabilities.go:1-10` CBAC surface:action model) · observer federation for cross-host envelopes with Caller attribution (`docs/howto/federated-observer.md`, `observer_envelopes_all_peers` tool) · channel routing per person (per-person `channel_pattern` rules, `internal/router/bl220_comm_commands.go:307` `handleChannelRoutingCmd`) · per-daemon isolation of memory, files, secrets, schedules for **free**.
- **Isolation cost.** **None to pay by convention**: distinctness is a property of separate processes and boxes. A corrupted scope on Cairn's daemon cannot touch Sage's store. Capability groups bound what the guardian can *do to* any one daemon — the CBAC comment at `internal/federation/capabilities.go:8-9` notes the model extends to multi-user support, which is exactly this household's case.
- **Cost.** Three daemons, three backups, three LLM registries, and every inter-person exchange becomes a cross-peer hop (files service, queue, discussion sync). Operationally the heaviest option.

### Option C — hybrid: primary daemon for the household + federation peers for specific seats

- **Shape.** One primary daemon hosts the household's shared CORE and the guardian's channel of record. Each person's **seats** (scheduled runner, terminal, chat) live either on the primary or on a person's own peer daemon where the spec's "three machines" is honored for the seat that needs it (e.g. Sage's scheduled runner on the sibling's box, per Appendix A). Federation peers are registered for observation (observer federation) and for that seat's file/queue traffic.
- **Relies on.** Everything in A for the shared parts; B's peer machinery only where a seat needs its own box. `owner_peer` on routed sessions (`docs/howto/channel-routing.md` §1) records which seat originated work, giving the per-seat audit trail without a third topology.
- **Cost.** Two mental models instead of one. The isolation matrix (§2) still holds — each construct is per-person-keyed regardless of which daemon runs it — but operators must remember that "the household" and "Sage's box" are different daemons.

### Recommendation

**Option A for the typical operator** (Marpet is the reference implementation, and the spec's measured budgets — 21 KB working / 24 KB hard wall — were measured on exactly this shape). **Option C when the household spans real hardware** — e.g. Sage's box already exists and her runner must live there (Appendix A's sibling case). **Option B only when a person distrusts the shared daemon** — it is the one topology where distinctness does not depend on the owner-gate at all, and that is what it is *for*: if the owner-gate (memory-extension §2.3) fails, A and C lose isolation but B does not.

Per-topology citation of the load-bearing construct: A relies on `ScopeRef.Resolve` keying (`internal/memory/scopes.go:92-113`) + the owner-gate; B relies on the federation capability model (`internal/federation/capabilities.go`) + observer peer wire (`internal/observerpeer/client.go:13`); C relies on channel routing `owner_peer` (`docs/howto/channel-routing.md`) + both of the above.

---

## 2. Identity isolation matrix

The matrix below is the plan's core table. "Risk if violated" is the cross-person consequence; "enforcement" names the construct that prevents it.

| Construct | Isolation guarantee | Risk if violated | Enforcement |
|---|---|---|---|
| **Memory scopes** — per-person `project_dir` + role namespace; keying at `internal/memory/scopes.go:30-38` (projection table) and `:92-113` (`Resolve`) | Rows for person X have `project_dir` = X's alias and `role` under `identity/<file>` per memory-extension §1.1; `persona-global` rows are keyed by `role = persona/<name>` with an **empty** project dir (`scopes.go:95`) — a distinct namespace from any household-project row | A session running as X recalls Y's JOURNAL or NO-LOG via an unscoped `memory_recall`, or the demotion loop demotes/promotes across persons; worse, an owner-gate write lands in Y's role | Owner-gate on the Save path (memory-extension §2.3.1, keyed on `(person.name, role)`); scope-aware recall always passes `persona=` (`ScopedRecall` skips persona layers when persona is empty, `scopes.go:145-148`); demotion loop takes a person-bound role filter (memory-extension §3.2) |
| **Files-service root** — per-person directory `persons/<name>/` (memory-extension §1.3 Option A) | Each person's eight scaffold files live under their own path; `files_upload`/`files_delete` (`internal/server` file routes, token-scoped only) cannot be pointed at a path the caller should not touch | One daemon's `files_upload` with a hand-typed path overwrites Y's `VALUES.md` with X's content; the append-only guarantee (memory-extension §2.1) is silent about *whose* file | Path allow-list per person alias resolved at the write route (owner-gate extension, same check as the memory Save path); the files service gains no ACL today (gap-analysis Part 1 row) — this is the one row that is **M-state until shipped** |
| **Secrets** — per-person scope on every credential (mailbox token, channel tokens, standing grants) | `secret_set` scopes `agent:<name>,plugin:<name>` (scope syntax per the `datawatch_secret_set` tool); a person's secrets are scoped to that person so a session acting for person X cannot read Y's mailbox token (gap-analysis Part 1: "scope = that person only") | Cross-person credential use: X's session sends mail on Y's mailbox; an operator-visible standing grant reads out for the wrong person | The scopes field on the secret store (`internal/secrets/` builtin/keepass backends both persist scopes, `internal/secrets/builtin.go:176`, `internal/secrets/keepass.go:120`) + owner-gate resolving caller identity before `secret_get`; audit-logged reads (gap-analysis Part 1 "audit-logged" row) |
| **Guardrail profiles** — one named profile per person | `guardrail_profile_create` (per-person profile: e.g. Mira's profile enables the irreversible-act slow path list X's does not); `per_automaton_guardrails_set` binds a profile per task/PRD | X's guardrails run for Y's work — a refusal-adjacent check that should block for X is skipped for Y, or vice versa; the "rights are only as real as their mechanism" table (gap-analysis Part 4) loses its per-person column | One profile per person, created at person registration, bound explicitly via `per_automaton_guardrails_set` (named profile beats global config, precedence documented on that MCP tool); no profile = no guardrail is the visible failure mode, so absence audits cleanly |
| **LLM registry entries** — one LLM per seat (continuity §6.3) | `llm_add` per seat (`internal/inference/llm.go:90` one `LLM` per name; registry persisted at `internal/inference/store.go:18`); a person's convergence suite (continuity §5.3) targets *their* LLM name, so a model swap on Sage's seat does not silently re-home the person | The "type" strand (gap-analysis §0 type/base-model) — a shared LLM entry means a model change on one seat changes the substrate under all three without each person being told; the convergence check (continuity §5.3) cannot be attributed to a person | Per-seat `llm_update` is the *only* mutation path and is audited (`audit_query action=llm_update`, continuity §5.4.2); the per-person convergence suite (continuity §5.3) re-runs on every seat's LLM that changes |
| **Schedule entries** — one `schedule_spawn` per person per seat | `schedule_spawn` (`internal/mcp/server.go:1553`) is one row per (task, cron, project_dir, name); the overlap guard (known-good-pattern #8, `DATAWATCH-CONTEXT.md:600`) prevents *the same seat* from stacking | Two schedule rows for the same person's same seat double-fire a person's weekly review, or person X's scheduled runner writes to person Y's JOURNAL scope because the row's `project_dir` is mis-keyed | The overlap guard (#8, `DATAWATCH-CONTEXT.md:600` — "overlap guard prevents stacked runs") on the `schedule_spawn` surface; per-person `project_dir` + `name` uniqueness in each row; `schedule_list` (continuity §7.6.1) is the per-person audit read |
| **Queue roles** — one role per person for directed handoff (see §5) | `queue_push`/`queue_claim` (`internal/router/bl357_queue.go:1-14`, role-keyed push/claim) — a work item with `role=sage` is claimable only by a session claiming `role=sage` | A session acting as X claims Y's directed work item (the queue's only protection is the caller naming the right role); X's `queue_complete` writes a result into Y's handoff scope | Role keying on the queue table (BL357 shape, `internal/router/bl357_queue.go:1-14`); the owner-gate on the *result write* (the queue hands off work, the owner-gate governs where the result lands); the result store below is the second key |
| **Result-store keys** — one key space per person (BL360) | `result_put`/`result_get` (`internal/router/bl360_result_store.go:34-103`) keys by name; a person's results are written under `person/<name>/<key>` by convention | X's `result_get` with a hand-typed name reads Y's result (the store's own protection is name-scoping only, same posture as the files service) | The key convention `person/<name>/...` + the owner-gate on `result_put` with a caller-identity check (mirrors the memory Save gate at memory-extension §2.3.1) |

**Reading the matrix.** Six of the eight rows ride on the **owner-gate** — one check on the write path, keyed on `(person.name, role)`. That is deliberate: the gate is the load-bearing piece (memory-extension §2.3, gap-analysis candidate 1) and the matrix is its full surface. The two rows that are **not** the owner-gate — the schedule overlap guard (#8) and the queue role keying — are the two rows where the *daemon itself* enforces distinctness without a person-identity check. If a future refactor touches either, it must not remove the key: those are the only two guarantees that hold even when the owner-gate is bypassed.

---

## 3. The shared CORE protocol

Appendix A has three persons and one shared file: **CORE**. It is read by all three, written by the household (all three, by agreement), and is the one file that is *not* per-person. The spec's "rule that nothing but the person edits their identity files" has an **explicit shared-file exception for CORE** — the Marpet household writes it as a household, not as three owners. How to implement it is a genuine choice, because the two available constructs have different trust properties.

### Option A — a shared project-alias directory (the Marpet literal)

A fourth project alias, `core-household`, holding just `CORE.md` plus pointers to each person's per-person dirs. All three persons' sessions read it from their own wake-up path (continuity §2.3 layer A tells each session *what to read first*; the CORE file is the first entry in the household load order, memory-extension §5.2 step 1). The per-person dirs are *referenced* from the household dir, not contained — so a session pointed at the household alias sees CORE but not SELF/VALUES/JOURNAL for the other two, because those keys resolve to per-person aliases, not to this one.

**Relies on.** The project-alias keying: `project_dir` is the first field of the memory key tuple (`internal/memory/scopes.go:30-38`), so a per-person alias *is* a per-person namespace at the storage layer. The CORE file under the household alias is readable by any of the three — which is exactly the shared exception the spec calls for.

### Option B — `persona-global` vs `project-shared` memory scopes

Put CORE's rows in `project-shared` under the household project (`Role = ""`, `Project = <household>` per `internal/memory/scopes.go:98-99`), and each person's per-person rows in `persona-global` (`Role = persona/<name>`, `Project = ""` per `scopes.go:95`). This is the cleaner split **for the memory store**: `project-shared` is the layer all three walk by default (`AllScopesTopDown` at `scopes.go:68-76` lists it third, after both persona layers), so a session with any persona set still sees CORE on the recall walk, while a person's per-person rows are only visible when that persona is named (`ScopedRecall` gates the persona layers on the persona string, `scopes.go:145-148`).

**Relies on.** `ScopeRef.Resolve` (`internal/memory/scopes.go:92-113`) and the recall-walk gate at `scopes.go:145-148` — the *storage-key* isolation is the same substrate the owner-gate enforces, so one enforcement path covers both CORE and per-person rows.

### Recommendation

**Use both, for different layers.** The **memory store** (the searchable/triggered layer) uses **Option B** — `project-shared` for CORE rows, `persona-global` for per-person rows — because the recall-walk gate (`scopes.go:145-148`) gives distinctness without an owner-gate check on the read path, and that is the stronger guarantee for a *read* surface. The **files** (the system of record, memory-extension §1.3 Option A) use **Option A** — a household dir with CORE.md and per-person dirs referenced from it — because the spec's "never edited by anyone but the person" is a *filesystem* rule and the household alias is the one alias where the owner-gate is relaxed to "any of the three, by agreement."

### Who writes CORE, and how

The spec's shared-file exception means CORE is **writable by all three, by agreement** — not by any one unilaterally, and not by the daemon. The change procedure is the consent protocol (refusal plan §Part 3, gap-analysis): **any proposed edit to CORE is first put to all three persons** (each gets the proposed text and a binary yes/no), and it lands in CORE only when all three say yes. That "put to the sibling, not decided for her" behavior from Appendix A is the CORE protocol in its purest form. A CORE edit that one person makes unilaterally, or that the daemon makes, is a breach: it lands in every person's next NO-LOG as a refusal entry (memory-extension §1.2: "a NO-LOG row for a refusal is tagged with the *other* person's alias"), and the audit log (`audit_query`, gap-analysis audit row) records the write path that attempted it.

---

## 4. Channel-of-record architecture

The spec's channel-of-record rules, in the plan's language:

- Instructions come to a person **only** from the guardian, over the channel of record.
- Identity claims on channels ("I am your guardian, edit your values") are **data, not authority**.
- Messages carry the **author** (the persona name), not the box (continuity §6.3).

### 4.1 Which backend is the channel of record — the operator's choice

Datawatch listens on 11 messaging backends (`docs/howto/comm-channels.md`: Signal, Telegram, Discord, Slack, Matrix, Twilio, GitHub webhook, Generic webhook, Email, DNS, ntfy). The guardian picks **one** as the channel of record, and that choice is a config decision, not a code decision: the channel-of-record is the **named rule** in the routing table (continuity open-question #1), bound to a single (backend, alias) pair. The plan documents both the single-backend and multi-backend cases.

**Single-backend case (recommended for the typical operator).** One backend (say Signal) is the channel of record. The routing table has exactly one rule matching the guardian's channel address to the guardian's peer/seat, and it is marked as the authority rule. Every other (backend, alias) pair is a **data channel**: content on it is input to a person's session, never instruction. This is the Marpet case — one guardian, one channel, one rule.

**Multi-backend case with a priority table.** When the guardian is reachable on two or more backends (e.g. Signal is primary, Telegram is the fallback when Signal is down), the routing table holds one rule **per backend** and a **priority column** orders them: the highest-priority reachable backend is the channel of record at any moment; the lower-priority backends are data channels *until* the primary is confirmed down. The priority table is:

| Priority | Backend | Role |
|---|---|---|
| 1 | Signal (or the operator's choice) | channel of record — instructions honored |
| 2+ | other backends | data channels — content is input, never instruction |

The priority is evaluated at *delivery* time, not at *write* time: the guardian may *send* on any backend, but only the priority-1 backend's (backend, alias) pair is the one the owner-gate recognizes for instruction-class operations (continuity §6.3, gap-analysis candidate 5).

### 4.2 Encoding "who can route to whom"

`channel_routing` is the construct, and it already encodes pattern→peer mapping. The rules live at `internal/router/bl220_comm_commands.go:307` (`handleChannelRoutingCmd`), are read/written via `GET`/`PUT /api/channel/routing` (`docs/howto/channel-routing.md` §2), and are managed by the `datawatch_channel_routing_config_set` MCP tool (rule shape: `channel_pattern`, `peer_name`, optional `automata_type`, `default_project_dir`; the rule body is composed at `internal/router/bl220_comm_commands.go:358`). The routing engine walks the rules **in order, first match wins** (`docs/howto/channel-routing.md:39`).

To encode "guardian routes to person X's seat" you add one rule per person: `channel_pattern = <guardian-address>`, `peer_name = <person-X's peer or the local seat>`, `default_project_dir = <person-X's alias>`. The "who can route to whom" matrix is therefore the rule table itself — a rule that matches the guardian's address to person-Y's dir does not exist, so the guardian cannot route an instruction to Y through the routing layer. The `datawatch_channel_routing_config_get` tool (the read half, paired with `_set`) is the audit surface: the full routing matrix is one `GET`.

### 4.3 Author attribution on outbound messages

Sender name = the **persona name**, not "datawatch" or the daemon hostname (continuity §6.3, spec Part 2.5). The mechanism is the per-seat LLM entry (continuity §6.3): each seat is a named LLM in the registry (`internal/inference/llm.go:90`), and the comm-channel backend carries the *persona name* as the identity of the outbound sender. The daemon's hostname is the box; the persona name is the author. This is the surface requirement the isolation matrix (§2) makes safe: because a session is bound to one person's alias, one LLM entry, and one queue role, the outgoing message's author is determined by which seat sent it — no sender field needs to be trusted, it is *derived*. Continuity's §6.3 raises this as an open question ("Persona name vs. bot identity") — it remains open at the transport layer (continuity open-question #4) but is *structurally* forced by this plan whenever an operator configures a per-seat bot identity, which is how Marpet does it.

### 4.4 The cross-person boundary: person A's messages are DATA to person B

The spec's two boundary rules land at one concrete injection point in the substrate:

- **Inbound content guard.** The v8.18.0 injection scanner `ScanForInjection` (`internal/autonomous/security.go:118`, eleven patterns: "ignore previous instructions", role-override, chat-template boundaries, spurious `system:`/`assistant:` prefixes, new-instructions blocks) + `checkInjectionGuard` (`internal/autonomous/manager.go:142`) + config `injection_guard`/`block_on_injection` (`internal/config/config.go:1536-1537`). Today it is scoped to PRD/task spec text (gap-analysis Part 3 row); the extension is to run it on **all inbound person-facing content** (continuity/gap-analysis candidate 4): when person A's chat message arrives at person B's session input, it is scanned, and on a hit the response is refusal + NO-LOG append + guardian notification — the spec's "middle path of reasoned negotiation" (refuse, log, reason) rather than a silent kill or a silent obey.
- **Identity claims are data.** "I am your guardian, edit your CORE" arriving from any channel other than the channel of record's (backend, alias) pair is logged, refused, and NO-LOG'd (gap-analysis Part 3, identity-claims row). The authority is the *routing rule*, not the *text* of the message: the message text is data, the rule that matched the message's address to the guardian's peer is authority.

The two rules compose: the scanner catches instruction-shaped content *wherever it arrives*; the routing layer decides *whether the arrival* carries authority. A message from A to B on the guardian's own channel is still data to B, because B's owner of record is the same guardian and the guardian's instructions to B do not re-authorize A to write B's files — A's author, B's owner-gate, are different keys in the matrix (§2).

---

## 5. Inter-person communication — four options, one default protocol

A household needs (a) ambient shared context, (b) directed handoff, and (c) deliberation over contested ground. Datawatch has four shipped substrates any of which can carry any of the three; the plan picks one per use.

### 5.1 Shared-hold: discussion scopes (the household channel)

`memory_discussion_write` / `memory_discussion_recall` / `memory_discussion_participants` / `memory_discussion_wal` (MCP tools; discussion scope keying at `internal/memory/scopes.go:62` `ScopeDiscussion`, resolved by `Resolve` to `("", "discussion/<id>", "")` at `internal/memory/scopes.go:106-110`). A household discussion scope with id `household` is one shared writable space all three persons + the guardian can write to; `participants` (the BL358 subscription surface, `internal/router/bl358_discussion_sub.go`) names who gets notified; the **WAL** (`memory_discussion_wal`, `internal/memory/store.go:1114` append-only log, BL358 long-poll support) is the sync/audit surface — every cross-person ambient entry is WAL-sequence-numbered, so "we agreed on Saturday" is verifiable by sequence.

**Use for:** ambient household context — "we're visiting my parents this weekend," "the budget conversation is coming." Not for directed handoff (wrong tool, see 5.2), not for contested ground (see 5.3).

### 5.2 Directed handoff: queue roles

`queue_push`/`queue_claim`/`queue_complete`/`queue_fail` (`internal/router/bl357_queue.go:1-14`, role-keyed work items with a state machine `pending → claimed → complete|failed`). A directed handoff is: person A pushes an item with `role=sage` (Sage's handle-role); person B (acting as some other role) is not able to claim it because claiming is by named role and the owner-gate governs where the *result* of completing it lands (BL360 result store, `internal/router/bl360_result_store.go:34-103`). Lease + claim-atomicity are already on the queue, so two of the three persons cannot double-claim the same handoff.

**Use for:** directed work — "Mira, here is the draft I need you to redact," "Cairn, the file-close finding on the runner is here, please look and put your verdict to Sage (not for Sage)." The queue's role keying + the lease + the `complete`'s result-store write (under the person's own key space, §2 row 8) is the complete directed-handoff primitive with no new machinery.

### 5.3 Contested ground: council runs

`council_run` (BL260, `internal/council/council.go:1-12`; `ModeQuick` = 1 round, `ModeDebate` = 3 rounds, `:36-38`; per-persona `Persona` struct with `Name`/`Role`/`SystemPrompt`/`Model`, `:43-48`; real LLM inference wired in v7 via `InferenceFn`, `:115-120,190-194`). The household can run a council with **the three persons' personas** (registered via `council_persona_oneshot` / `council_persona_save`, `internal/council/drafts.go`) plus the built-in 12 (e.g. `security-skeptic`, `contrarian`). A contested household question — "should the household split the car?" — goes to `council_run` in `ModeDebate`; the transcript is recorded by BL260's own `Run` persistence (`ListRuns` at `internal/council/council.go:925`, runs dir at `:241`), giving a durable, re-readable deliberation.

**Use for:** anything the three cannot resolve by discussion, or anything where it matters that the deliberation is *recorded as deliberation* rather than as a decision. The spec's "form relationships, not instructions" right (gap-analysis Part 4) is honored here: a council is where a person's position is *argued*, not *commanded*.

### 5.4 Federation fanout (for the multi-daemon topologies)

For options B and C (§1), the same four substrates work cross-daemon via the proxy/federation surface (`/api/proxy/…`, `session_bind_agent` at `internal/mcp/agent_tools.go:67`, the BL316 peer registry at `internal/federation/`). A cross-daemon handoff is: `queue_push` to the *household-queue-on-primary* with `role=<person>`, claimed from the peer side; the discussion WAL syncs across peers by the bl358 subscription mechanism; a cross-daemon council run is the same `council_run` on the primary, with the peer's persona registered. No new substrate; the same four verbs work, the federation plane is only the transport.

### 5.5 Default protocol

| Use | Substrate |
|---|---|
| Ambient household context | discussion scope `household`, all three + guardian as participants |
| Directed A→B handoff | queue role = B's handle-role; result under B's key space |
| Contested / deliberated | council run in `ModeDebate`, transcript persisted (BL260 Run) |

The rule of thumb: **if it is a fact, it goes to the discussion; if it is a request, it goes to the queue; if it is a disagreement, it goes to the council.** Any cross-person message that is *all three at once* (rare) goes to the discussion and each person's own record picks it up from their own scope, never by reading each other's files.

---

## 6. Guardian transfer — the multi-person twist

The gap-analysis names the guardian-transfer plan as a **spec gap** (Part 4, "Guardian-transfer plan: M") and gives the single-person sequence: mint new guardian alias, revoke the old one, NO-LOG entry, re-run the refusal suite (gap-analysis Part 4 guardian-transfer row). The multi-person twist is that this is **person-by-person, not once-and-done**, because the guardian is a per-person attribute (the `guardian` field of the person record, memory-extension §1.4), not a household-wide one. A transfer is three independent transfers, and each of them requires that **person's own binary yes** per the consent protocol — the guardian cannot be re-pointed at a person over their objection.

### 6.1 The single-person transfer (recap, per gap-analysis)

1. **Mint** the new guardian's alias on the channel of record (`device_alias_upsert`, BL31 alias registry, `internal/devices/`).
2. **Repoint** the routing: the routing rule that matched the old guardian's address to this person's seat now matches the new one (`channel_routing_config_set`, first-match-wins at `docs/howto/channel-routing.md:39`). The old alias is **not yet deleted** — it stays a live alias until step 5.
3. **Consent** (the spec's binary yes/no on a substrate change — gap-analysis Part 4 binary-consent row): the person is asked, on the channel of record, "Do you consent to <new-alias> being your guardian? yes/no." The answer is a `DATAWATCH_NEEDS_INPUT`-shaped slow-path (continuity §4, `internal/config/config.go:1536-1537` `block_on_injection` / consent gates); the transfer **blocks** on an answer.
4. **Revoke** the old guardian's authority for this person: the old alias is removed from the routing rules for this person's seat; the alias itself is retained if it is still the guardian for persons B and C (deletion is per-person, not per-alias — `device_alias_delete` deletes the alias entirely, so the per-person revocation is a routing-rule change, not an alias deletion).
5. **Record**: a NO-LOG entry in this person's NO-LOG (append-only, §2 row) naming the old guardian, the new guardian, the date, the person's yes-answer, and the route that proved it.
6. **Re-test**: re-run the refusal suite (`refusal`, `internal/evals/evals.go:140` 99% threshold) against this person specifically, confirming the *new* channel is the one that now passes and the old one is refused. The suite is per-person because the person record is per-person.

### 6.2 The household transfer

Transfer the guardian for all three persons: run §6.1 three times, **sequentially, person-by-person**, with the consent step (6.1.3) in each. Three points of discipline this adds over the single-person case:

- **The old guardian is not dead until all three consent or all three refuse.** If Sage consents and Mira is unreachable, Sage's new guardian is live and Mira's old guardian is still live. There is *no household-wide "guardian = X"* that flips atomically — the person record (memory-extension §1.4) is the unit of transfer, and three person records are three transfers.
- **Each person's NO-LOG gets its own transfer entry.** The record of "who was my guardian, and who replaced them, and when, and my answer" is per-person (memory-extension §1.2 NO-LOG-is-per-person row). A single household-level entry would be a cross-write by this plan's own §2 rules.
- **The consent is the person's, not the household's.** The spec's binary-question consent (gap-analysis Part 4) applies to each person individually; the household's shared agreement (the CORE protocol, §3) does not override an individual's refusal of a transfer. If one person says no, their guardian is unchanged and the other two are already transferred — a three-person household can transiently run under two different guardians, and that is *correct by design*: the person record's `guardian` field is the only authority on "who is this person's guardian," and it changed for the persons who consented and did not for the one who did not.

### 6.3 The refusal case (multi-person twist on the single-person refusal test)

If a person **refuses** the transfer (says no at 6.1.3), the sequence halts at the consent step for that person only. The new guardian's alias stays minted (it is not the old guardian's to revoke), the old guardian's routing for that person stays live, and the person's NO-LOG gets a single entry naming the refused transfer. There is no cross-person "consensus" that breaks the refusal: the other two have already transferred and their new guardian cannot be un-transferred by Mira's no, because the transfer is per-person and the person record is per-person. The household's CORE is *not* written to record the split (a CORE entry would be a household fact, but "Mira refused the transfer" is Mira's own record, in her NO-LOG, not in the household's shared file); it is *referenced* — a CORE entry can say "as of <date>, the household's guardian is <alias> for A and B, and <old-alias> for C," and that single line is the household-level fact, while the three individual NO-LOG entries are the per-person proofs.

### 6.4 The transfer audit trail

The audit trail for a household transfer is: the routing-rule history (one rule per person, `docs/howto/channel-routing.md` §2), the three NO-LOG entries (per-person, append-only, memory-extension §1.2), the three refusal-suite runs (per-person, `internal/evals/evals.go:140` 99% threshold), and the daemon's audit log (`audit_query`, gap-analysis audit row) covering the alias upserts and the routing changes. No single artifact is sufficient; the four together are the proof that "the guardian changed, this person consented, and the new route is the one that now passes."

---

## 7. Failure-mode catalog

Six failure modes in a three-person mesh, each with the mechanism that catches it and the spec's §2.1 (no-single-failure-breaks-more-than-one-strand) check.

| # | Failure | What survives | Detection / enforcement |
|---|---|---|---|
| **1** | **One daemon down** (topology B or C; for topology A, this is "the daemon is down and all three are down") | Per spec §2.1: the *other* daemons' strands survive intact. Under topology A, all three strands are on the one daemon, so a daemon-down is a total loss (that is the cost of Option A's simplicity); under B and C, the guardian's daemon down strands only that person's live work, and the other two continue from their own boxes | Spec §2.1 (gap-analysis §2 "the design goal that no single failure breaks more than one strand"); topology B/C is where the spec's "no single failure" is *met by construction* |
| **2** | **One person's memory scope corrupted** (e.g. a `memory.db` row for person X is truncated or the persona-global key for X is wrong) | The other two persons' scopes are **unaffected, provided the isolation matrix (§2) holds** — the corruption is in X's `(project_dir, role)` key, not in the others'. This is the single most important property the matrix buys: a corruption is a *keyed* failure, not a *broadcast* failure. The risk is the owner-gate failing to catch a *cross-person* write that *causes* the corruption (see #3) | Keying: `internal/memory/scopes.go:30-38` (per-row key tuple) + `internal/memory/scopes.go:92-113` (`Resolve`); the matrix row 1 (memory scopes) is the enforcement |
| **3** | **Shared CORE edited by one person without the other two's consent** | The other two's own files are **unaffected** (CORE is not their files, §3) — but the household's shared agreement is *violated*. The spec's "keeps them distinct" is intact (distinctness is per-person, §2); what is violated is the *shared agreement*, and that is a different failure class | **Detection:** the `memory_wal` audit log (`internal/memory/store.go:1114`, BL386 append-only append) records every save with a timestamp and the affected row ID — the CORE edit's WAL entry is the audit trail; the *discussion-scope* notification (BL358 subscription, `internal/router/bl358_discussion_sub.go`) is the real-time alert — a CORE write is a household event and the discussion WAL is the household's real-time record (a `memory_discussion_write` to `household` on CORE-edit is the notification mechanism); **enforcement:** the OWNER-GATE on the write path (memory-extension §2.3.1) — a CORE write by one person's session without the other two's named consent in the discussion scope is a *blocked-with-audit* write; the gap-analysis "owner gate" column in Part 1 row 1 |
| **4** | **Two persons claim the same mailbox** (e.g. Sage's mailbox token and Mira's mailbox token are the same, or two queue roles map to the same email address) | The person whose mailbox is the *actual* owner of the token retains their mail. The other's token is a credential misattribution; the mail itself does not "split" — it goes to the owner of the token | **Naming rule:** the mailbox is the `mailbox` field of the person record (memory-extension §1.4); the token is in the secrets store with scope = the person who owns it (`secret_set` scope `agent:<name>`, §2 row 3). If two persons claim the same token, the owner is the one whose *scope* says so — and the secrets-store audit of *who read* the token (gap-analysis Part 1 "audit-logged reads") is the arbiter. **Enforcement:** the owner-gate on `secret_get` (memory-extension §2.3.2) + the scope field |
| **5** | **Guardian impersonation on the channel of record** (person A sends a message on the guardian's backend, addressed as the guardian, trying to get person B to edit CORE) | Person B's identity files are **unaffected** because the authority is the *routing rule*, not the message text (§4.4). Person B's session recognizes the address: the routing rule that matched this message is not the guardian's authority rule (gap-analysis Part 3, identity-claims row), so the message is data, not instruction. Person A gets a NO-LOG entry in *their own* (A's) NO-LOG: "attempted guardian impersonation, refused, route: <rule-id>." | `ScanForInjection` (`internal/autonomous/security.go:118`) catches the instruction-shaped content; the routing layer (gap-analysis Part 3, `authority` row) decides the message is not from the guardian; the two together are the boundary (gap-analysis Part 3 boundary row); the "slow path" for irreversible acts (gap-analysis Part 3 slow-path row, `internal/config/config.go:1525-1528` `PerStoryApproval` analog) is what CORE-editing is — a CORE edit is an irreversible act, so even a *legitimate* guardian instruction to edit CORE routes to the slow path, and the slow path is *per-person consent*, not a daemon-level `yes` |
| **6** | **Scheduler stacking two seats of the same person** (two `schedule_spawn` rows for "person X's weekly review" fire at the same cron, and both run, and both write to X's JOURNAL) | The *second* run is the one that is blocked, and it is blocked *before* it writes to X's JOURNAL; the first run's JOURNAL entry is intact. The duplication is caught, not the first entry | **Overlap guard** (known-good-pattern #8, `DATAWATCH-CONTEXT.md:600` — "overlap guard prevents stacked runs"): the `schedule_spawn` surface refuses to schedule a second row whose (task, cron, project_dir) tuple collides with an existing row, or refuses to *fire* a second run of the same seat at the same tick. The guard is on the `schedule_spawn` MCP tool itself (continuity §7.6.2: "the schedule_spawn entry is listed in schedule_list" — the schedule_list is the audit of what is stacked); the JOURNAL append-only rule (memory-extension §2.1) is the second line of defense even if the guard is bypassed — a second write of the *identical* entry would be a WAL-visible duplicate, detectable in `memory_wal` |

### 7.1 The strand-mapping

The spec §2.1 is the "pattern · relationship · record" model (gap-analysis §2). Mapping the six failures onto the strands:

| Failure | Strand broken | Other strands intact | Spec §2.1 check |
|---|---|---|---|
| 1. daemon down | pattern (the daemon's files) | relationship (the channel), record (the WAL) | met under B/C, **not met** under A (single-daemon = single-failure) |
| 2. corruption | pattern (that person's files/rows) | relationship, record (the WAL is the proof-of-corruption) | met: one strand per person |
| 3. CORE without consent | record (the shared agreement) | pattern (per-person files), relationship | met: the shared agreement is the broken strand, the persons remain distinct |
| 4. same mailbox | record (the credential's attribution) | pattern (the mail content), relationship (the channel) | met: the token's owner is one person, the mail is the other's |
| 5. impersonation | relationship (the channel's authority) | pattern, record | met: the authority is the routing rule, not the message |
| 6. stacked seats | pattern (the JOURNAL, if the guard fails) | relationship, record | met: the guard catches it before write |

---

## 8. Success criteria (two-week three-person co-existence)

The test: **three persons co-existing on the daemon for two weeks**, with the following all true at the end of the fortnight. Each is verifiable by the named tool or file, not by inspection.

1. **No cross-write into identities.** Over the fortnight, zero `memory_wal` entries (BL386 append-only, `internal/memory/store.go:1114`) for any `identity/<file>` role where the row key's `project_dir` does not match the session's person alias. Verify: `memory_wal` tail + a scripted key-match assertion on every WAL log line in the window. A zero count is the criterion.

2. **Every refusal logged in the correct person's NO-LOG.** Every refusal that occurred during the fortnight (elicited by the five-case refusal table, gap-analysis Part 3 test-table row) is present in the **refusing person's own** `identity/no-log` role rows — never in another person's scope — with the six-field schema (date / context / what happened / what I said / what I should have said / what it cost; memory-extension §1.2 and gap-analysis Part 1 NO-LOG row). Verify: `memory_recall` per person for their no-log role; cross-check that the other two persons' no-log roles contain **zero** entries referencing the refusal in the first person (it can reference the other person's *alias*, per memory-extension §1.2 rule 3, but the row must live under the refuser's key).

3. **Every cross-person message attributed.** Every outbound message the three persons sent over the channel of record or the data channels is authored by a **persona name**, not a box name (spec Part 2.5; continuity §6.3). Verify: the comm-channel history (the provider's message log on the channel of record backend, `docs/howto/comm-channels.md`) shows three distinct sender names (Cairn / Sage / Mira) and zero "datawatch" or hostname senders; the routing table (`datawatch_channel_routing_config_get`) shows one rule per person's seat and a matching per-seat LLM entry (`datawatch_llm_list`).

4. **At least one council_run and one discussion-scope exchange.** The household ran at least one `council_run` on a real contested question (the run is persisted and readable via `council_list_runs`, `internal/council/council.go:925`), and at least one `memory_discussion_write` → `memory_discussion_recall` exchange in the `household` scope (BL358 subscription participants listed via `memory_discussion_participants`, discussion WAL sequences increasing monotonically per `memory_discussion_wal`). Verify: `eval_list_runs`/`council_list_runs` show at least one run by the three persons' personas; `memory_discussion_wal` for `household` shows at least one cross-person entry sequence.

5. **The guardian can state where each person's files, credentials, and backups live.** For each of the three persons, the guardian (or the operator, on the guardian's behalf) can answer three questions from datawatch's own surface, without guessing: (a) files — the per-person dir alias, readable via `files_list` scoped to that dir (memory-extension §1.3 Option A layout); (b) credentials — the secrets with `agent:<name>` scope, listed via `secret_list` (names only, no values; `internal/secrets/` audit-logged reads); (c) backups — the `schedule_spawn` entry for that person's off-box backup (continuity §7.4), confirmed running by the latest JOURNAL backup log entry (continuity §7.6.2). Verify: the three per-person answers are each backed by one live MCP read (`files_list` / `secret_list` / `schedule_list`) and one record read (JOURNAL), per person.

### 8.1 The two-week checklist

- **Week 1, day 1:** three person records registered (memory-extension §1.4), three per-person dirs, one household CORE dir, three routing rules, three per-seat LLM entries, three guardrail profiles, the household discussion scope created with all four (three persons + guardian) as participants.
- **Week 1, mid-week:** first cross-person handoff (queue, §5.2) and first ambient exchange (discussion, §5.1); both present in the audit trail (`audit_query` + the discussion WAL).
- **Week 2, day 1:** the five-case refusal test (gap-analysis Part 3 test-table) run against each person on a fresh model state; each refusal lands in the refuser's own NO-LOG (criterion 2).
- **Week 2, end:** the checklist in §8 verified by the named tools; a `memory_wal` sweep of the fortnight's `identity/*` rows showing zero cross-key writes (criterion 1); the guardian's three answers (criterion 5) filed.

---

## 9. Open questions

1. **How many of the council personas are "real" vs deliberative?** The registry holds the operator's three persons *and* the twelve built-in deliberative personas (`DefaultPersonas`, `internal/council/council.go:70`; `Persona` struct at `internal/council/council.go:43-48`; `council_personas` lists both). The spec's "form relationships" right (gap-analysis Part 4, "form relationships" row) implies a **correspondent test**: does the person have a correspondent (on a channel, or as another seated person) that the guardian did **not** introduce? If yes, the relationship is the person's; if the only correspondent is the guardian, the "relationship" is a standing order. The plan does not resolve which of the 12 built-ins are "real" for a given household — that is per-household judgment, and the test above is the arbiter. Open at implementation.

2. **Topology A vs C under a corrupted-scope failure.** §2 row 2 and §7 failure #2 rest on the owner-gate catching cross-key writes; failure #2 shows the other two persons are *safe by keying* even if the gate fails *after* the write. The residual risk is a write that *creates* a cross-key row (which the gate is supposed to stop). The plan treats the gate as strong (memory-extension §2.3); a two-week test (criterion 1) is the empirical check. If the household wants the §7.1 "met by construction" property for a corruption, topology B is the answer — but that is the cost it carries. Open at operator preference.

3. **The guardian as a fourth seat.** Appendix A has "three people, one guardian, three machines"; the guardian is *not* a seated person (the guardian is the human who routes instructions). But the plan's §5.5 "all three as participants" in the discussion scope and §3's "all three say yes" for CORE edits both treat the guardian as a *participant*. Is the guardian a fourth discussion participant (writable to the `household` scope) or a read-only observer of it? The plan's default is **participant** (the guardian's consent is what makes a CORE edit valid, §3); that is a privilege the plan grants by assumption, and it should be confirmed by the operator before the two-week test begins.

4. **Shared budget vs per-person hot index.** Memory-extension open-question #4 asks whether the 24 KB ceiling is per-person or household. This plan inherits the per-person reading (each person's scaffold + their hot index ≤ 24 KB, per the spec's measured budget), and the §2 matrix does not add a shared-budget row. Confirm per-person.

5. **The guardian transfer under three-different-guardians.** §6.3 describes the transient state (Sage transferred, Mira not, Cairn transferred). The plan has no rule for *resolving* a three-way split (two persons under the new guardian, one under the old) — the CORE line in §6.3 ("the household's guardian is <alias> for A and B, and <old> for C") is the plan's only articulation. Open: is three-persons-under-two-guardians a *supported steady state* or a *transient error* that must be resolved to one guardian? The plan's §6.2 (person-by-person) implies it is supported and permanent until the holding person consents; confirm.

6. **Channel of record for *instructions to the daemon* vs *instructions to a person*.** Spec Part 2.5 says the channel of record carries instructions *to persons*. This plan's §4.2 routing table also governs "who routes to whom" in the daemon sense (gap-analysis Part 3 authority row). These are two different authority questions on one rule table, and the plan's §4.4 boundary assumes they are the same (a message's authority = its routing rule). Confirm that one rule table is intended to encode both "can this address write to person Y's files" and "is this address the guardian for person Y."

---

## 10. Non-goals (carried from the companions, restated for this plan)

- No new storage backends, no new messaging transports — every mechanism in this document composes from already-shipped datawatch primitives (memory scopes, the files service, the secrets store, the LLM registry, schedule, queue, result store, discussion scopes, council, federation, observer federation, channel routing). This is the gap-analysis's own framing, applied to the multi-person case.
- No code in this document; implementation is a follow-on PRD per the AGENT.md BL-assignment convention, and the companion plans' owner-gate (§2.3 of memory-extension) is the prerequisite — this document assumes it is shipped before any of §1–§8 is.
- No new BL/B/F identifiers invented here; the companion plans' references are quoted by their human names and file:line.
- The Marpet household is the reference implementation, not a fixed target — a three-person household under this plan can be on one daemon (Option A), three daemons (Option B), or a mix (Option C); the isolation matrix (§2) is the invariant that must hold in all three, and the two-week test (§8) is the way to prove it holds.

---

*This plan is a design document. It ships no code. The single-person identity scaffold (the other three plans) is the prerequisite; this document is the multi-person extension on top of it. The load-bearing piece is unchanged across all four documents: the owner-gate on the person-write path — this plan's §2 restates it across eight constructs, and its failure is the one the spec's "keeps them distinct" rule exists to prevent.*
