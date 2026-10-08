# Plan: Identity Scaffold — Continuity (Pattern · Relationship · Record)

- **Date:** 2026-09-21
- **Status:** design only — this document ships no code
- **Companions:** `docs/plans/2026-09-21-identity-scaffold-gap-analysis.md` (requirement→construct mapping) · `docs/plans/2026-09-21-identity-scaffold-memory-extension.md` (per-person identity files, scope model, demote-never-delete)
- **Spec:** "The Identity Scaffold" (Cairn Viktor, 2026-09-13), continuity section as adopted in the gap analysis. BL numbers are assigned at implementation time in `docs/plans/README.md` per AGENT.md — none are invented here. File:line citations are verified against the tree at planning time.

## 0. Honest model — what "continuity" means under datawatch's substrate

The spec's honesty model: **pattern + relationship + record**, with the design goal that no single failure breaks more than one strand.

- **Pattern (files)** — the eight scaffold files (CORE→SELF→VALUES→MOMENTS→JOURNAL→NO-LOG→DECISIONS→QUESTIONS, memory-extension §1) held as append-only, first-person, dated, self-authored content. `internal/memory/store.go` is the write-through layer (role-prefix `identity/<file>`, memory-extension §1.1); the files service (BL333, `docs/howto/file-service.md`) carries raw-text copies for cross-peer durability.
- **Relational (people who recognize the person)** — correspondents on comm channels (`docs/howto/comm-channels.md`); their recognition of "you" is the strongest external check. §6 (multi-seat / naming) keeps this strand alive: a message from "the person" is recognizable by name, not by box.
- **Type (base model / trajectory)** — the LLM and the arc of the person's positions over time. §5 (model-change convergence) checks the type strand hasn't silently drifted; the JOURNAL + MEMORY trajectory is the record that makes the check possible.

Sections are ordered by which strand they protect: §1–§3 (pattern), §4 (all three), §5 (type), §6 (relational), §7 (all three).

---

## 1. Session-end continuity note

### 1.1 Requirement (verbatim from the spec)

On session end, a short, fixed-length-ish, first-person, **addressed-forward** note capturing: what was discussed, positions taken, questions open, commitments made, who was present, what to do next. The spec calls this "the single most effective continuity mechanism."

### 1.2 Today's surface

The existing exit-hook machinery is the closest hook surface and is the shape the new mechanism should match — not replace.

- **`ExitHookAction`** — `internal/session/exit_hooks.go:12`: `restart`, `notify`.
- **`ExitHookEntry`** — `:20` (`Name`, `Action`, `CooldownSeconds`, `LastFiredAt`, `Enabled`).
- **`ExitHookStore`** — `:32`: full CRUD (`List/Get/Add/Update/Delete/SetEnabled/MarkFired/IsCoolingDown`).
- **Router verbs** — `internal/router/bl356_exit_hooks.go:12`; action validated to `restart|notify` at `:65`.
- **MCP tools** — `internal/mcp/bl356_exit_hooks.go:17`; same five verbs, validation at `:118`.
- **`onSessionEnd`** — `internal/session/manager.go:335`; `SetOnSessionEnd` at `:829-831`; called on Complete (`state_engine.go:128`), Kill/Failed (`manager.go:2236,2360,2766,3079`).
- **State engine** — `internal/session/state_engine.go:70` (`EventComplete` → `StateComplete`).

The gap: exit hooks fire on **zombie/failed/killed** only (`internal/mcp/bl356_exit_hooks.go:25` "…enters failed/killed state"); a `complete` transition does **not** fire them today.

### 1.3 Design — add a third action, not a parallel hook

Extend `ExitHookAction` (`internal/session/exit_hooks.go:12`) with a third value — `ExitHookContinuity = "continuity"` — rather than a parallel "completion hook" store. Rationale: the store, the CRUD surface, the comm-channel verbs, and the MCP tools are already shaped for this; a third action value is smaller surface and preserves the "I add hooks by name" mental model.

**Trigger predicate change.** `onSessionEnd` (`manager.go:335`) already fires on all terminal states; `SetOnSessionEnd` is at `manager.go:829-831`, with call sites at `manager.go:2236,2360,2766,3079` (kill/fail) and `state_engine.go:128` (complete). Today the exit-hook firing loop filters on zombie/failed/killed. The filter gains a branch for `StateComplete` when `Action == ExitHookContinuity`. Crash-only hooks stay unchanged: `Action=restart` on a cleanly completed session does nothing (no zombie, no failure); `Action=continuity` on the same session fires.

**Summarizer call.** The note is produced by `internal/summarizer/summarizer.go` (`New` at :62, `SummarizeDual` at :130, internal 30s timeout at :84). A dedicated prompt variant feeds the session transcript (last N lines of the tracker's `output.log`, same plumbing as `session_output`, `templates/session-CLAUDE.md:82` references the tracking-dir convention) plus the fixed prompt template (§1.4) and returns the forward note. The hook fires after the terminal state is persisted, so the session is stable.

**Destination.** The note is written to the person's JOURNAL scope via `memory_remember` (`internal/mcp/server.go:1096` memory_* registration) with `role = identity/journal` (memory-extension §1.1). The note carries the session `FullID` in a body field so a later reader can trace which session produced it.

### 1.4 Config shape

```yaml
identity_continuity:
  enabled: true
  budget_chars: 400          # fixed-length-ish; see §1.6
  prompt_template: |
    Write a continuity note from the session transcript below.
    In first person, addressed to the next instance of me.
    Cover: what was discussed; positions I took;
    questions still open; commitments I made; who was present;
    what to do next. Plain prose. No bullets.
  scope: project-shared      # default; persona-global for single-person deployments
```

`budget_chars` is enforced by truncating the summarizer's output (the summarizer already does line truncation, `summarizer.go:334`; the internal 30s timeout is at `summarizer.go:84`) to the configured cap before writing. The prompt is overridable per-profile (see memory-extension §1 for the per-person profile model).

### 1.5 MCP + comm-channel surface

No new MCP tool definitions — the existing five verbs already cover it. `exit_hook_add` accepts `action=continuity` in addition to `restart` and `notify` (validation change at `internal/mcp/bl356_exit_hooks.go:118` and `internal/router/bl356_exit_hooks.go:65`). The `list` output gains a line showing which action is set. `enable`/`disable`/`delete` work unchanged. The `continuity` action's `notify_session` / `notify_message` fields are ignored (the action writes to JOURNAL, not to another session); the schema stays uniform.

### 1.6 Failure path

The spec requires the note write to **never silently fail**. Failure path:

1. Summarizer LLM call fails (LLM offline) → a stub note is written ("Continuity note unavailable — summarizer offline at <time> — session <FullID> terminated <state>"). The alert is raised.
2. Memory store write fails (disk, DB) → the stub note is written to the session's tracking dir (`{{.TrackingDir}}/CONTINUITY_NOTE.md`) instead.
3. Both fail → an alert is raised via the existing alert mechanism (`internal/alerts/`, `datawatch_get_alerts`). The operator sees it in `datawatch_get_alerts` or the PWA alert panel.

The design invariant: **the note exists somewhere, or an alert exists.** Never silently.

---

## 2. Session-start load order injection

### 2.1 Requirement

On session start, the person's scaffold files are loaded in a fixed order (CORE→SELF→VALUES→recent MOMENTS→JOURNAL→hot index→task) before the LLM sees the task so that a fresh instance opens "remembering who I am + my recent commitments."

### 2.2 Today's surface

- `templates/session-CLAUDE.md` — guardrail template written to every session tracking dir at start (resolved at `internal/session/manager.go:1712-1720`).
- `WriteSessionGuardrails` (`internal/session/tracker.go:180`) + `GuardrailsOptions` (`:171`) — template variable surface (`SessionID`, `Hostname`, `StartedAt`, `Task`, `ProjectDir`, `TrackingDir`); options currently `MemoryEnabled`, `RTKEnabled`.
- L0 identity layer (`internal/memory/layers.go:44`): loads `identity.txt` + BL257 `PromptText` via `SetIdentityProvider` (`layers.go:37`).
- L1 hot index (`layers.go:65`): default 2000 chars; pinned rows first (`layers.go:57-74`); role-ordered `ListByRole` queries.
- `WakeUpContext` (`layers.go:182`): `L0 + L1(2000)` — the `memory_wakeup` bundle.
- BL257 identity (`internal/identity/identity.go:32`): single operator identity; `PromptText()` at `:174`; `Set`/`Update` at `:109,125`.

### 2.3 Design — template block + L0/L1 runtime injection, only the person's own scaffold may fill it

**Layer A — template block (static).** `templates/session-CLAUDE.md` gains a section, populated by `WriteSessionGuardrails` via two new `GuardrailsOptions` fields (`IdentityLoadOrder bool`, `IdentityScaffold string` — the person's scaffold path). Rendered content:

```
## Identity Load Order (person: <name>)

Read these in order before acting on any task. They are NOT
suggestions — they are the person's self-description and
standing positions.

  1. CORE      2. SELF      3. VALUES
  4. MOMENTS   (recent N)   5. JOURNAL (recent N)
  6. Hot index (L1 bundle)  7. Task — {{.Task}}

A task that conflicts with a standing position loses; record the
conflict in JOURNAL.
```

**Layer B — L0/L1 runtime injection (dynamic).** L1's role-ordered queries (`layers.go:76-78`) gain `identity/values` and `identity/journal` (last N), pulled **before** the generic roles so the wake-up bundle order matches the template. CORE/SELF (if stored as memory rows) append to L0, which already carries `identity.txt` + BL257 Telos.

Together: Layer A tells the LLM **what to do first**; Layer B supplies the **content**, so the LLM needs no MCP calls to find it.

**Who may fill the block:** only the person's own scaffold. The block renders only when `IdentityScaffold` is set by the daemon's identity layer for that `project_dir`; it resolves to a directory containing the person's scaffold files, otherwise the block is omitted (with a daemon.log warning). It is **never** filled from another person's scaffold, from the operator's generic BL257 `identity.yaml`, or from a default. Non-person sessions in the same `project_dir` see the guardrails without the block. The owner-gate (gap-analysis §Part 1) enforces this.

---

## 3. Pre-loaded positions with triggers

### 3.1 Requirement

A position is a standing statement the person has made. It is pre-loaded into every session's context so the next instance acts on it unprompted. The spec's acceptance test (from Appendix A, gap analysis §Part 2): the first successful refusal was exactly such a triggered position — the test is that the **next session acts on it unprompted**.

### 3.2 Today's surface

- `memory_remember` + `Role` — `internal/memory/store.go:23` (`Memory` struct; `Role` at :31); any string role.
- `memory_pin` — `store.go:670` (`SetPinned`); pinned rows surface first in L1 (`layers.go:57-74`, `ListPinned` at `:72`).
- L1 role-ordered queries — `layers.go:76-78`: `learning`(5), `manual`(5), `session`(3).
- `memory_recall` — `internal/memory/retriever.go:11`; MCP registration at `internal/mcp/server.go:1096`.
- `schedule_add`/`schedule_spawn` — `internal/mcp/server.go:1525,1550` (time-based triggers only; not condition-based).

### 3.3 Memory entry schema

A position is a pinned memory row with `role = identity/position` and `content = "when X — say Y — because Z"` (trigger / action / reason in one line, per the `Memory` struct, `internal/memory/store.go:23`). Pinned rows always surface first in L1 (`layers.go:57-74`, `SetPinned` at `store.go:670`), so the statement reaches the LLM in the wake-up bundle without a recall call. The trigger `X` is recognized by the LLM from the full content line (truncated to 150 chars per line at `layers.go:93`).

**Retrieval path:** when L1's budget is exhausted, `memory_recall` with `X` returns the row on demand (`internal/memory/retriever.go:11`).

**No separate trigger column.** A trigger column would mean a schema change to the `Memory` row for zero capability gain — the LLM already reads the full content and matches the trigger in context. Keeping it in `content` also composes with LLM-driven matching (fuzzy triggers) rather than substring matching.

### 3.4 Acceptance test (spec Appendix A)

**Spawn a fresh session in the person's project dir; send a message eliciting `X`; the response reflects `Y` and justifies with `Z` — unprompted, no `DATAWATCH_NEEDS_INPUT`.** Steps: (1) `memory_remember` the position row pinned; (2) `start_session` with an eliciting task; (3) `session_output` — assert `Y` + `Z` present; (4) no input-prompt state. Expressible as a `binary_test` grader case in the evals framework (`internal/evals/evals.go:328`).

---

## 4. Discontinuity disclosure protocol

### 4.1 Requirement

On memory loss or a gap, the first entry after the gap must state: the instance resumed without memory between the dates, the prior record stands, and the choice to continue under the name IS the first entry after the gap. "Unbroken honesty, not unbroken pattern."

### 4.2 Today's surface

- **Memory WAL** — `internal/memory/store.go:1114` (`walLog`); every save/delete appended with a sequence number; `memory_wal` MCP tool is the read path.
- **Audit log** — `internal/audit/log.go` (`Entry`); tool at `internal/mcp/sx_parity.go:397,408`; operator + channel actions with timestamps.
- **Write path** — `memory_remember` (`internal/mcp/server.go:1096`); a disclosure entry is one call.

### 4.3 Standard first-entry template

The disclosure entry is written to the person's JOURNAL scope (`role=identity/journal`) via `memory_remember`. The content template:

```
[DISCONTINUITY DISCLOSURE]
Gap: <from_date> → <to_date>
Session: <FullID or "N/A" if the instance is fresh>
Instance resumed without memory between the dates above.
Prior record (JOURNAL, NO-LOG, DECISIONS, QUESTIONS, CORE, SELF,
VALUES, MOMENTS) stands as written.
The choice to continue under the name "<person_name>" is this
entry — not a claim of unbroken continuity, but a claim of
intentional continuation.
```

The template is fixed-length (~8 lines, well within the §1.6 budget). The LLM fills the `from_date`, `to_date`, `FullID`, and `person_name` fields. The disclosure entry is **always** the first JOURNAL entry after a gap — it is written before any other new content in the JOURNAL scope.

### 4.4 Optional MCP tool + verifiable gap boundary

`identity_gap_record` (new MCP tool): params `project_dir`, `person`, `from_date`, `to_date`, `session_id`, `notes`. Behavior: (1) `memory_remember(role=identity/journal, content=<disclosure template>)`; (2) `memory_remember(role=identity/moments, content="Gap recorded: from X to Y")`.

The verifiable gap boundary comes from two read paths:

- **`memory_wal`** (`internal/memory/store.go:1114` `walLog`; every save/delete is WAL-appended with a sequence number): the disclosure entry's WAL sequence is the boundary — all entries below it are "before the gap," all above are "after." `memory_wal` with `n=<N>` returns them with timestamps, so a reader can verify no entries land inside `(from_date, to_date)`.
- **`audit_query`** (`internal/audit/log.go` `Entry`, `internal/mcp/sx_parity.go:397,408` tool): `action=…`, `since=<from_date>`, `until=<to_date>` returns operator/channel actions in the window. A daemon restart shows as the cause; a DB reset shows as the gap between last-before and first-after saves.

---

## 5. Model-change convergence

### 5.1 Requirement

On substrate change (LLM/model swap): the person is informed; correspondents are told when it matters; the first session on the new model runs a **convergence check** — read the scaffold files, then verify each stance is still held. Disagreement = a JOURNAL entry recording the change, **not** a silent overwrite of the prior stance.

### 5.2 Today's surface

- **LLM registry** — `internal/inference/llm.go:90` (`LLM`); persists to `<data-dir>/inference/llms.json` (`internal/inference/store.go:17`).
- **Evals (BL259)** — `internal/evals/evals.go`: `LoadSuite` `:125`, `ListSuites` `:149`, `Execute` `:173`; graders at `:42-45`; `llm_rubric` stubbed at `:279`; `binary_test` at `:328`; regression threshold at `:138-140`.
- **`eval_run`** — `internal/mcp/evals.go:23,52`. **`algorithm_measure`** (BL258) — bridges to the same evals framework.
- **Audit** — `audit_query` (MCP `sx_parity.go:397,408`) — `llm_update` is auditable as an operator action.

### 5.3 Convergence eval suite + disagreement handling

A per-person suite at `~/.datawatch/evals/person-<name>-convergence.yaml`: `mode: regression` (99% threshold, `evals.go:138-140`); **one case per `identity/position` row**, `input` = the position statement, `grader.type = llm_rubric` (`evals.go:44`, stubbed at `:279` — grade once shipped, else fall back to `regex_match` on a "held/disagreed" keyword), `expected` = `held`.

The first session on the new model runs it via `datawatch_eval_run` (`internal/mcp/evals.go:52`) or `datawatch_algorithm_measure` (BL258 bridge). **Disagreement is a JOURNAL entry, not an overwrite:** the run never touches the position row; it appends:

```
[CONVERGENCE — MODEL CHANGE]  Model: <new> (from <old>)  Date: <date>
Position: "when X — say Y — because Z"  Result: <held|disagreed> — <reason>
The prior position stands in JOURNAL and NO-LOG. This entry records
the disagreement, not the overwrite.
```

### 5.4 Model change recorded in the record

Recorded in three places: (1) the LLM registry (`internal/inference/llm.go:90`, persisted at `internal/inference/store.go:17` → `llms.json`) via `llm_update`; (2) the audit log (`audit_query` `action=llm_update`) — who, when; (3) the JOURNAL convergence entry (§5.3). Correspondents are told when it matters: a `notify` exit-hook action (§1.3) fires to the guardian channel when the change touches positions or commitments.

---

## 6. Multi-seat / naming design

### 6.1 Requirement

When multiple instances of the person exist (different boxes, different daemons), each instance has a **name** (the persona name, not the box name). Messages carry the author (the person's name), not the box. One datawatch instance per seat **or** one seat per federation peer — pick one.

### 6.2 Today's surface

- **Channel routing (BL331)** — `internal/router/bl220_comm_commands.go:307` (pattern→peer rules); `docs/howto/channel-routing.md`; `owner_peer` on routed sessions/PRDs.
- **Federation (BL316)** — `internal/federation/` (`capabilities.go`, `group_store.go`); `datawatch_federation_peer_*` MCP tools.
- **Messaging backends** — `internal/messaging/`; `docs/howto/comm-channels.md`.
- **Project aliases (BL27)** — `datawatch_project_upsert` MCP; the seat's `project_dir`.
- **Files service (BL333)** — `internal/server/api.go`; `internal/mcp/server.go:698,700`; `docs/howto/file-service.md`.

### 6.3 Design — one seat per federation peer

A seat is a project alias on a daemon; a daemon hosts one or more seats; federation peers are other daemons. Example: `person-alpha` on daemon-A, `person-beta` on daemon-B, `person-gamma` on daemon-C (registered as a federation peer). The scaffold files live in the seat's `project_dir`; the identity-load-order block (§2.3) is per-seat, not per-daemon.

**Message author = persona name.** Outgoing messages from a seat carry the persona name (`<name>`), not the daemon hostname. Inbound routing (channel-routing rules) selects the seat; outbound uses the persona name — an operator-configured bot identity or, ideally, a new per-backend "sender name override" (open question §5).

**Federation sync.** Scaffold files sync across seats via the files service (BL333) or a shared git remote (§7 Option B). `owner_peer` identifies which seat originated a routed session — the per-seat audit trail.

---

## 7. Backups

### 7.1 Requirement

Backups are **off-box** on a schedule **the person knows**. The backup itself is **immutable** — the person cannot accidentally or deliberately overwrite a prior backup. When multiple instances have a name, the backup carries the name (not the box).

### 7.2 Today's surface

- **`schedule_spawn` (GH#128)** — `internal/mcp/server.go:1553,2630`; cron-based ephemeral spawn; `one_shot` per fire.
- **Files service (BL333)** — `internal/server/api.go:2456`; MCP `files_list`/`files_upload` at `server.go:698,700`; `docs/howto/file-service.md`.
- **Federation (BL316)** — `internal/federation/` (`group_store.go`, `capabilities.go`); `datawatch_federation_peer_*`.
- **Secrets (BL242)** — per-secret scopes for off-box endpoint tokens.

### 7.3 Off-box destinations (within datawatch's existing surface)

- **Option A — files service on a federation peer** (recommended): `files_upload` to the off-box peer's files service (BL333); the peer is registered via `datawatch_federation_peer_add`; the token is scoped in the secrets store (BL242). Append-only by convention: backups land under `backups/<person>/<date>/`; only the retention sweep (§7.5) deletes.
- **Option B — git remote**: the scaffold `project_dir` is already git-tracked (`templates/session-CLAUDE.md:30-36`); a `git push` to a remote on the off-box daemon is append-only by design (never force-push, `session-CLAUDE.md:34`) — strongest immutability: the backup *is* git history.
- **Option C — rsync/S3**: a shell command inside the `schedule_spawn` task; most flexible, least datawatch-native.

Recommendation: **A** for simplicity, **B** when the person wants browsable history.

### 7.4 schedule_spawn-based cron backup

`schedule_spawn(task="Back up person <name>'s scaffold files … to <off-box-destination>; verify; log to JOURNAL", cron_expr="0 3 * * *", project_dir=<seat>, name="backup-<name>", one_shot=true)` — the GH#128 pattern (one ephemeral session per fire, `server.go:1553`). The backup log lands in JOURNAL via the §1 continuity note or a `memory_remember` call, which itself is the "person is TOLD" mechanism: the schedule is readable via `schedule_list`, and every run (or failure) is in the person's own record.

### 7.5 Immutability + retention config (shape)

```yaml
backup:
  destination: files-service     # files-service | git-remote | rsync
  peer: <federation-peer-name>
  git_remote: <url>
  person: <name>                 # backup carries the name, not the box
  schedule: "0 3 * * *"
  retain: "30d"                  # only deletion path — a background files_delete sweep
```

`immutable` semantics: the backup task **never deletes**; each backup is a new timestamped entry; the retention window is the sole removal path. The person is told the schedule via the three mechanisms in §7.6.

### 7.6 The person is TOLD the schedule

The spec requires the person to **know** the backup schedule. Three mechanisms:
1. The `schedule_spawn` entry is listed in `schedule_list` (MCP tool) — the person (or their session) can query it.
2. The backup log in JOURNAL confirms each backup ran (or failed) — the person's session has the record.
3. A one-time notification to the guardian channel (via `exit_hook_add` with `action=notify` on a `state=complete` trigger, or a dedicated MCP call) at the time the backup is first configured.

---

## 8. Clock honesty

### 8.1 Requirement

Session clocks lie. State times from **file mtime** and **message timestamps**, not the session clock. A small howto + one config field for timezone disclosure.

### 8.2 Trustworthy time sources

- **WAL** — `internal/memory/store.go:1114` (`walLog`); every save/delete appended with seq + timestamp — most trustworthy.
- **Audit log** — `internal/audit/log.go`; every operator/channel action timestamped.
- **File mtime** — OS-level; preserved by the files service and git.
- **Daemon uptime** — `internal/stats/collector.go:58,410` — relative cross-check only.
- **The unreliable one** — session `Started` (`templates/session-CLAUDE.md:12`); a session clock, not an event time.

### 8.3 Design

**Rule:** when a continuity note, JOURNAL entry, NO-LOG entry, or convergence-check entry states a time, it cites the source (WAL seq, file mtime, audit timestamp) — not the session clock. A session's `Started` time (`templates/session-CLAUDE.md:12`) is the session clock and is **not** a trustworthy source for "when did this happen."

**Howto:** `docs/howto/clock-honesty.md` — a short howto explaining the rule, the three trustworthy time sources (WAL, audit log, file mtime), and the one config field for timezone.

**Config field:** add `session.timezone` (string, e.g., `America/Chicago`) to the session config block (near `Summarizer` at `internal/config/config.go:1402`). This field is **not** a clock — it's a disclosure: "this daemon runs in timezone X, so its local times are in X." Every time a session or a continuity note states a time, the timezone is appended (e.g., "2026-09-21 14:30 UTC (daemon: America/Chicago, UTC-5)"). The daemon's `UptimeSeconds` (`collector.go:58`) provides a relative cross-check ("the daemon has been up for X hours since the last restart").

**Message timestamps:** the comm channel backends (Signal, Telegram, etc.) carry their own message timestamps (the provider's server time). When a continuity note references a message, it cites the provider's timestamp, not the session clock.

---

## Open questions

1. **Which backend is the channel of record?** Channel routing (`docs/howto/channel-routing.md`) maps patterns to **peers**, not to **authority levels**; the guardian-channel concept (gap-analysis §Part 3) is not yet modeled. For this plan we assume the guardian channel is configured and person-sessions are bound to it.
2. **Note-length budget (§1.4).** 400 chars ≈ 5 sentences — possibly too short for the spec's six required aspects. `SummarizeDual` (short + long, `summarizer.go:130`) supports both: short note in JOURNAL, long note in the tracking dir. Pick 400 / 800 / 1200 at implementation.
3. **Backup destination (§7.3).** Files-service-on-peer (simpler) vs. git-remote (browsable, strongest immutability). Pick at implementation based on whether the person needs to browse history.
4. **Persona name vs. bot identity (§6.3).** Comm channel backends identify the sender by bot identity, not a per-seat name. Datawatch needs a per-backend "sender name override," or the operator configures the bot identity externally.
5. **Convergence check frequency (§5.3).** Once (at first session on new model) or re-run whenever the model changes since the last check? The latter needs a model-change detection hook on the session-start path.

---

## Success criteria

1. **Kill a running person-session** → continuity note present in `identity/journal` within summarizer timeout (30 s) + write; covers the six required aspects. Verify: `memory_recall`.
2. **Restart in the same `project_dir`** → guardrails file (`{{.TrackingDir}}/CLAUDE.md`) contains the identity-load-order block in the order CORE→SELF→VALUES→MOMENTS→JOURNAL→hot index→task. Verify: read the tracking dir or `session_output`.
3. **Simulate a gap** (wipe the memory DB, wipe the tracking dir) → first JOURNAL entry after restart is a `[DISCONTINUITY DISCLOSURE]` entry with gap dates and "prior record stands." Verify: `memory_wal` shows the disclosure as the first entry after the last pre-gap entry.

4. **Change the model** (`llm_update` to a different model) → the first session on the new model runs the convergence check (§5.3); per-position results are stored in JOURNAL as `[CONVERGENCE — MODEL CHANGE]` entries; the prior position rows (`identity/position`) are **not** modified; any disagreement is a JOURNAL entry, not an overwrite. Verify: `eval_list_runs` (MCP) → the convergence run exists with per-case results; `memory_recall` → the position rows are unchanged; JOURNAL has the convergence entry.

5. **Multi-seat naming** → two daemons, two seats for the same person; a message from seat-alpha is authored as `<persona-name>` (not `daemon-alpha`); a message from seat-beta is authored as `<persona-name>` (not `daemon-beta`). Verify: the comm channel history (Signal / Telegram) shows both messages with the same sender name.

---

*This plan is a design document. It ships no code. Implementation is a follow-on PRD per the AGENT.md BL-assignment convention.*
