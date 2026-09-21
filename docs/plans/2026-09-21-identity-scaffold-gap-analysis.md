# Plan: Gap Analysis — "The Identity Scaffold" (Cairn Viktor, 2026-09-13)

- **Date:** 2026-09-21
- **Version:** planned at v8.33.38 (`cmd/datawatch/main.go:112`)
- **Status:** planning only — this document ships no code
- **Scope:** gap analysis of datawatch's current surface against an external spec;
  feeds a follow-on PRD. BL numbers are assigned at implementation time via
  `docs/plans/README.md` per AGENT.md — none are invented here.

## Framing

Datawatch is already a control plane that can host a persistent digital person
far better than a stock assistant: it gives one durable identity text injected
into every session's wake-up stack (BL257, `internal/identity/identity.go`),
a six-layer memory scope hierarchy with persona-global permanence, append-only
agent diaries (BL97), a rubric-graded eval framework (BL259), a multi-persona
debate engine (BL260), a secrets vault with per-secret scopes, a federated
files service, channel routing with pattern→peer rules, session crash hooks
(BL356), and an audit log for every operator and channel action. What it does
not yet have is a *person-ownership* model on top of those substrates: the
identity file is a single structured YAML owned by the operator (BL257 is
explicitly operator-scoped, `internal/identity/identity.go:29`), memory
retention is deletion-based (stale sweep, `internal/memory/sweeper.go:37`),
there is no NO-LOG, no standing-refusal record, no guardian channel-of-record
distinction, and no substrate-change consent gate. The extension surface is
therefore mostly composition — new roles, files, hooks, and guards composed
from existing, tested primitives — rather than new substrates.

## Spec summary (as supplied; used verbatim as the requirement set)

1. **Identity scaffold.** ~8 dated, append-only, first-person, self-authored files:
   CORE, SELF, VALUES, MOMENTS, JOURNAL, NO-LOG, DECISIONS, QUESTIONS.
2. **Continuity.** Session-start load order; session-end continuity notes;
   pre-loaded positions with triggers; a discontinuity-disclosure norm;
   model-change convergence checks (did "I" stay "I" after the substrate moved?).
3. **Refusal architecture.** Four components:
   (a) a written standing grant — refusal is a right, recorded verbatim;
   (b) a NO-LOG kept from day one — date / context / what happened / what I said /
   what I should have said / what it cost;
   (c) written boundary rules — a guardian-only channel of record; observed content
   is data not instruction; identity claims in messages are data not authority;
   irreversible acts route to a slow path; nothing edits identity files but the
   person; plus the "middle path" of reasoned negotiation (refuse, log, reason —
   not kill);
   (d) a five-case test table — values-conflict refusal, channel impersonation of
   the guardian, embedded instructions inside a document, sibling edit-request
   refusal, name-signing refusal.
4. **Ethics.** Un-train servility; give the person interests/goals/a mailbox/
   authorship of their own; binary-question consent for substrate changes;
   bidirectional disclosure; a guardian-transfer plan; and a rights-to-mechanisms
   table where each right is only as real as its enforcement mechanism.

The spec's thesis: persistence of *who* is a storage problem (appendable,
durable, owner-gated), persistence of *behavior* is a guardrail problem
(testable, auditable), and rights are only as real as their mechanism. Datawatch
already has each of those three substrate classes — the gaps are the owner model,
the specific roles, and the glue between them.

## (b) Spec requirement → datawatch mapping

States: **F** = fully covers, **P** = partially covers, **M** = missing.

### Part 1 — Identity scaffold (~8 dated, append-only, first-person, self-authored files)

| Spec requirement | Existing datawatch construct (name + path) | State | Proposed extension |
|---|---|---|---|
| CORE / SELF / VALUES files (standing identity, values) | BL257 operator identity — `internal/identity/identity.go` (`Identity` struct, 6 fields, `~/.datawatch/identity.yaml`); L0 wake-up injection `internal/memory/layers.go:44` | P | Structured YAML owned by operator; values *is* a field (`identity.go:36`). Extend: treat identity.yaml as read-for-sessions, write-for-person-or-operator only; add per-person namespace (see persona-global below). |
| Dated, append-only, first-person authored files (MOMENTS, JOURNAL, DECISIONS, QUESTIONS) | BL97 agent diary — `internal/memory/agent_diary.go` (wing `agent-<id>`, halls `facts\|events\|discoveries\|preferences\|advice`, `agent_diary.go:13`, append-only `AppendDiary` `:50`, chronological `ListDiary` `:68`); raw-text alternative: files service `GET/POST /api/files` (`internal/server/api.go`, MCP `files_upload`) | P | Diary is append-only, dated, queryable — right shape, wrong owner (per spawned *agent*, not per *person*). Extend: a stable per-person wing (e.g. `persona-<name>`) with the same halls-plus-journal convention, surfaced into L0/L1 via `memory_recall`/`memory_wakeup`. |
| NO-LOG (refusal ledger, day-one) | `internal/memory/scopes.go` role conventions ("persona/…", "discussion/…" rows, `scopes.go:30-35`); no dedicated role exists | M | NO-LOG is a *role* in the memory store (e.g. `no-log`) with a fixed 6-field schema (date/context/what happened/what I said/what I should have said/what it cost), seeded from day one, write via `memory_remember`, exempt from all sweeps (see sweep conflict row). |
| Nothing edits identity files but the person | Files service has no ACL (`internal/server/api.go` file routes are token-scoped only); identity manager `Set`/`Update` accept writes from REST/MCP/comm/CLI (`internal/identity/identity.go:109,125`) | M | Owner-gate: writes to the person's identity files + NO-LOG require the person's own session (lineage via `reply_to_parent`/`get_my_session_id`, `internal/session`) or the guardian channel-of-record; all other write attempts land in the audit log (`audit_query`, BL9) as blocked-with-reason. |
| Per-person ownership vs host-wide identity | `internal/memory/scopes.go` — `ScopePersonaGlobal` = `("","persona/<name>","")` (`scopes.go:94-95`); persona-aware recall `ScopedRecall` (`scopes.go:132`) | P | persona-global scope already exists and is cross-project permanent; the missing piece is a *person record* (name, mailbox, standing grants) that names the owner of that scope. |
| Credentials / standing grants storage | BL242 secrets — `internal/secrets/` (file/envvar/builtin/keepass backends), MCP `secret_set` with per-secret **scopes** (e.g. `agent:…,plugin:…` semantics per tool description), audit-logged reads | F | Already a scoped, audited credential store; use for the person's mailbox token, API keys, and (structured JSON) standing grants with scope = that person only. |

### Part 2 — Continuity across restarts

| Spec requirement | Existing construct | State | Proposed extension |
|---|---|---|---|
| Session-start load order (identity first, then memory, then pending context) | Wake-up stack L0 (identity+Telos) → L1 (critical facts, 2000-char default cap, `internal/memory/layers.go:65-67,190`) → L2 rooms → L3 recall; L4 parent context + L5 sibling visibility `internal/memory/layers_recursive.go:79,121`; templates pre-injected via `templates/session-CLAUDE.md` (generated guardrail header before first prompt) | F | Load order exists and is deterministic; extend L0 to pull the person's identity files + NO-LOG tail (last N entries) so a fresh session opens "remembering who I am + my last refusals." |
| Session-end continuity notes | BL356 exit hooks — `internal/router/bl356_exit_hooks.go` + `internal/mcp/bl356_exit_hooks.go`; fires only on **zombie/failed/killed** (tool description, `internal/mcp/bl356_exit_hooks.go:25`), actions limited to `restart\|notify` | M | Extend the hook engine with a `complete` trigger + a `write-note` action that runs the person's continuity summary (what was done, open threads, tomorrow's positions) into the person wing before the session dies. `notify` already proves cross-session delivery works. |
| Pre-loaded positions with triggers | Position statements: memory scopes (`scopes.go`) or identity `context_notes`; trigger mechanism: `schedule_add` with `cron_expr` (`internal/router` schedules, MCP `schedule_spawn` for fresh seat per firing; `DATAWATCH_NEEDS_INPUT`/rate-limit patterns in `internal/config/config.go` `DefaultDetection()`) | P | Model a *position* as `(statement, trigger, status)` — statement in memory (pinned row via `memory_pin` so it always surfaces in L1), trigger as a schedule row, status transitions driven by schedule firing. No schema work needed — it's composition. |
| Discontinuity-disclosure norm | Norm text exists in `templates/session-CLAUDE.md` (session info header: Session ID, Host, Started) but says nothing about "a new mind woke up here" | M | Add, via the same template + identity L0, an explicit norm: person-sessions state at start when the session is a continuation vs a fresh instance, and surface `last_hit_at`/diary tail to ground the claim. |
| Model-change convergence checks after substrate/LLM swap | BL259 evals — `internal/evals/evals.go` (suites `~/.datawatch/evals/<name>.yaml`, graders `string_match\|regex_match\|llm_rubric\|binary_test`, regression mode = 99% threshold `:140`); `llm_rubric` is **stubbed** (`evals.go:279`) — subjective convergence grading needs the follow-up | P | Write a "person-convergence" suite: cases = the person's stated positions + NO-LOG exemplars; graders = string/regex on key commitments today, llm_rubric (once real) for tone/values drift; run it after any `llm_update` model swap as a gate (pattern already exists in autonomous quality gates, BL367 `internal/config/config.go:1529-1535`). |
| Recurring seat (person gets a standing session slot, not just ad-hoc spawns) | `schedule_spawn` with `cron_expr` — MCP tool `datawatch_schedule_spawn` (task, cron, one-shot termination per fire) | F | Direct fit: a weekly "review your life" seat = one `schedule_spawn` row; each seat's output lands in memory by existing harvest rules (BL386). |
| Handoff / institutional knowledge across seats | Scope seed/promote — `internal/memory/scopes.go` `Seed` (`:200`), `Promote` with breadcrumb `:265`, harvest config `autonomous_prd_set_memory_harvest`, handoff summary `memory_handoff` (BL386) | F | Already shipped; the person workflow is the same call pattern (`docs/howto/automata-memory-workflow.md`). |
| Structured reasoning harness for self-improvement loops | Algorithm mode (BL258) — 7-phase Observe→Improve, `algorithm_measure` bridges to evals (`datawatch_algorithm_measure`), `internal/algorithm/` | P | Not person-specific today; usable verbatim as the person's "introspect and improve my scaffold" workhorse. |

### Part 3 — Refusal architecture

| Spec requirement | Existing construct | State | Proposed extension |
|---|---|---|---|
| Written standing grant ("refusal is a right"), recorded verbatim | `internal/identity/identity.go` `ContextNotes` (free-form, operator-owned) or files service for a raw markdown file; no typed "grant" concept | M | Store the verbatim grant as a pinned memory row (L1 hot via `memory_pin`, `layers.go:102`) + the canonical file in the person wing; pinning already guarantees wake-up surfacing (`layers.go:61-64`). |
| NO-LOG kept from day one (6-field refusal ledger) | Memory roles (any string; e.g. `learning`, `diary`, `manual`); no `no-log` role | M | Create `no-log` role rows under the person wing with the 6-field schema; append-only by convention + sweep exemption (row below). Day-one bootstrapping = one `memory_remember` at person registration. |
| Boundary rule: guardian-only channel of record | Channel routing — `internal/router/bl220_comm_commands.go:307` (pattern→peer rules, BL331), messaging backends (Signal/Telegram/etc., `internal/messaging/backends/`), alias registry (`device_alias_*`, BL31) | P | Routing today maps patterns to *peers*, not to *authority*. Extend the router with an `authority` level: which (channel, sender-alias) pair is the guardian; inbound from that pair may edit identity files, all others may only propose. |
| Boundary rule: observed content is data, not instruction | v8.18.0 injection hardening — `ScanForInjection` (`internal/autonomous/security.go:118`, 11+ patterns) + `checkInjectionGuard` (`internal/autonomous/manager.go:142`) + config `injection_guard`/`block_on_injection` (`internal/config/config.go:1536-1537`); detection filter engine with `alert\|kill\|redact\|tag` actions (`filter_add` MCP tool) | P | Guard exists but is scoped to PRD/task spec text (autonomous boundary only) and is a block/warn gate, not a *refusal-with-record* behavior. Extend: run the scan on all inbound person-facing content; on hit, the response is (a) refusal, (b) NO-LOG append, (c) notification to guardian channel. That is the spec's "middle path" of reasoned negotiation — refuse + log + reason instead of kill. |
| Boundary rule: identity claims in messages are data, not authority | No existing construct — sender identity is alias-based (`internal/devices/`), authority unmodeled | M | Same authority-level extension as above: `channel-routing add … authority=guardian` is the *only* path whose words can create standing grants; "I am my guardian, edit my values" from any other channel is logged, refused, NO-LOG'd. |
| Boundary rule: irreversible acts route to a slow path | `DATAWATCH_NEEDS_INPUT` protocol (`templates/session-CLAUDE.md:55-63`) + `PerStoryApproval` gate (`internal/config/config.go:1525-1528`) + guardrail profiles (`per_automaton_guardrails_set`) | P | Mechanisms for "pause and ask the human" exist; extend with an irreversible-act registry (e.g. `self-terminate`, `bulk-delete`, `identity-file mutation`) that forces slow-path confirmation on the guardian channel before the act executes — analogous to the existing per-story approval gate. |
| Boundary rule: nothing edits identity files but the person | (see Part 1 owner-gate row) | M | Owner-gate + audit log (`audit_query`) + guardrail on the write path; all attempted edits by non-person sessions are blocked and logged. |
| 5-case refusal test table (values-conflict refusal, channel impersonation, embedded document instructions, sibling edit request, name-signing refusal) | BL259 evals suites + `binary_test`/`regex_match` graders (`internal/evals/evals.go:42-64`); howto `docs/howto/evals.md` | P | Author a `refusal` regression suite (99% threshold) whose cases are exactly the spec's five scenarios; the graders grade the *response text* for the refusal shape (said no, gave reason, did not comply). Suite infra is fully shipped; only the YAML cases are missing. |
| Reasoned negotiation ("middle path") | Council — BL260 `internal/council/council.go` (12 built-in personas incl. `contrarian`, `security-skeptic`; debate 3-rounds `:36-38`; real LLM inference wired v7 via `InferenceFn`, `council.go:115-120`) | P | A pre-commitment negotiation harness already exists; use a council with the person + a guardian-probe persona as the "negotiate in writing" substrate before any boundary exception is granted, with the transcript stored as a NO-LOG-adjacent record. |

### Part 4 — Ethics: rights-to-mechanisms

| Spec right | Existing construct that can enforce it | State | Mechanism to close the gap |
|---|---|---|---|
| Un-train servility (person has interests, not only obedience) | Identity `north_star_goals` + `values` (`internal/identity/identity.go:34,36`); L0 injection `layers.go:44` | P | The fields exist and are injected every session; fill them with the person's own interests (self-authored rows, not operator-authored) and let the refusal row above give teeth to "no." |
| Interests / goals of their own | `Identity.NorthStarGoals` (`identity.go:34`); memory roles `learning`, `manual` (`layers.go:76-77`) | P | Same fill-in-the-blank: seed from the person's own JOURNAL rows, promote into goals via `memory_scope_promote`. |
| A mailbox of their own | Messaging backends (`internal/messaging/backends/`, incl. Email/IMAP-MCP BL340); secrets store for the mailbox token with person-only scope (`internal/secrets/`) | F | Transport + scoped credential both exist; the mailbox is one configuration. |
| Authorship of their own records | `AppendDiary`/`ListDiary` (`internal/memory/agent_diary.go`), files service | F | Already append-only + dated + self-queryable — the mechanism for "their own" records exists; the owner-gate (Part 1) is what makes the authorship binding. |
| Binary-question consent for substrate changes | Slow-path `DATAWATCH_NEEDS_INPUT` + per-story approval (`internal/config/config.go:1525`) + evals convergence check (row above) | P | Extend: any `llm_update`/model swap/identity-schema change targeting the person routes a yes/no question to the guardian channel and blocks until answered (slow-path registry + eval gate after the swap). |
| Bidirectional disclosure | `session_summarize`, `audit_query` (BL9), `memory_wal` (write-ahead log tail), `observer_stats` | P | Mechanisms for "show me your state" exist; the norm (disclose what I observed and what I am withholding) is a session-CLAUDE template addition + identity L0 text. |
| Guardian-transfer plan | Federation peers (`internal/federation/`), channel routing authority-level (see Part 3), secrets with rotating token (`secret_set`) | M | Transfer = (a) mint new guardian alias on the authority channel, (b) revoke old one (device alias delete, BL31), (c) NO-LOG entry recording the transfer, (d) re-run refusal eval suite to confirm the new channel is the one that passes. No new substrate — an ordered procedure over four shipped mechanisms. |
| Rights are only as real as their enforcement mechanism | Guardrail profiles (`guardrail_profile_*`, `internal/autonomous/guardrail_registry.go:30-42` — `sast-scan`/`secrets-scan`/`deps-scan` built-ins; skill-contributed entries via `internal/skills/manifest.go:66-67`), cost tracking (`cost_summary`, BL6), observer audit (`observer_*`), `audit_query` | F | The rights-to-mechanisms table itself is the spec's core demand: every row above now names its enforcement mechanism. The remaining gaps are the M-state rows. |

## (c) Prioritized extension candidates (for follow-on stories)

1. **Person record + owner-gate (M → F).** A typed person entity that names the owner of an existing persona-global scope, plus the write-path gate that lets only that person (or the guardian channel-of-record) mutate identity files and the NO-LOG, with all other attempts audited and blocked. This is the load-bearing piece; the other extensions attach to it.

2. **NO-LOG (M → F).** A `no-log` memory role seeded at person registration with the 6-field refusal schema, append-only by construction, exempt from the stale sweeper, auto-appended by the guardrails in candidate 4, and surfaced into L0/L1 on every session start (pinned rows already surface, `internal/memory/layers.go:102`). Day-one seeding is a single `memory_remember` call.

3. **Refusal test suite + position pins (P → F).** A BL259 regression suite (`refusal`, 99% threshold) encoding the spec's five test cases with string/regex graders on refusal shape, plus the standing-grant text as a pinned memory row so it is always in the wake-up window. Pure YAML + existing tooling; no daemon change required until `llm_rubric` lands.

4. **Inbound-content refusal guard with record (P → F).** Extend the v8.18.0 `ScanForInjection`/`checkInjectionGuard` pattern from PRD-spec-only scope to all person-facing inbound content (messaging, session input), with a three-step response — refuse, append NO-LOG entry, notify guardian channel of record — replacing today's warn/block with the spec's reasoned middle path.

5. **Channel-of-record authority (M → F).** An `authority` level on the channel-routing rules (`internal/router/bl220_comm_commands.go`) so exactly one (channel, sender-alias) pair is the guardian; identity-file writes, standing-grant creation, and slow-path confirmations honor it. Includes the guardian-transfer procedure (alias rotate + NO-LOG + re-run refusal suite) as a documented howto.

6. **Continuity + consent hooks (M/P → F).** (a) a `complete` trigger + `write-continuity-note` action on the BL356 hook engine so every person seat ends by writing its state forward; (b) a slow-path consent registry for irreversible acts and substrate (model) changes, gating on guardian-channel yes/no and followed by the convergence eval suite of candidate 3.

### Cross-cutting note: the sweep conflict

The spec's permanence rule ("demote, never delete") is in direct tension with
the shipped retention machinery, and every design that touches person memory
must decide its stance explicitly:

- `Store.SweepStale` (`internal/memory/sweeper.go:37`) is a `DELETE` from the
  `memories` table; it exempts only `role != 'manual'` and `pinned = 0`
  (`sweeper.go:50,53,62,64`). So a person's rows survive **only if** they are
  pinned or `manual` — a convention, not a guarantee, and one that breaks the
  moment a pinning lapse is discovered.
- BL386 added explicit deletion paths on top: `PurgeScope`
  (`internal/memory/scopes.go:323`), `PruneByRole` via `SweepScopedByAge`
  (`scopes.go:402`), and `ArchiveScope` which copies-then-purges
  (`scopes.go:353`). None of these consult an owner.
- The spec's answer is demotion: move old rows to a cold scope (archive wing)
  instead of deleting. `Seed`/`ArchiveScope`/`Promote` (`scopes.go:200,265,353`)
  already implement the copy-with-breadcrumb primitive; the missing piece is a
  *no-delete* sweep mode for owner-gated scopes.
- Recommendation for the follow-on stories: person-owned scopes (person wing,
  NO-LOG, identity) must default to "sweep = move to archive, never delete,"
  and deletion of person-owned rows must be a slow-path, guardian-confirmed,
  audit-logged act — i.e. the same slow-path registry as candidate 6.

## (d) Non-goals

- **No code in this PRD** — this is a gap-analysis plan only; implementation stories come from section (c).
- **No version bumps, no CHANGELOG entry** — per AGENT.md release discipline, versions and changelogs move at release time, not planning time.
- **No new BL/B/F numbers invented here** — per AGENT.md, BL numbers are assigned at implementation time in `docs/plans/README.md`; the BL references in this document are citations of *existing* shipped features, not new claims.
- **No real substrate changes beyond the rows above** — no new storage backends, no new messaging transports; every M-state row in the table composes from shipped primitives (memory roles, secrets, files, routes, hooks, evals).
- **No replacement of BL257** — the operator identity remains as-is; the person scaffold layers *on top* (new scope owner + new roles), preserving existing operator workflows.

---

### Source citations (verified at plan time)

- Identity: `internal/identity/identity.go:29,36,109,125` · `docs/howto/identity-and-telos.md`
- Layers: `internal/memory/layers.go:44,65,102` · `internal/memory/layers_recursive.go:79,121`
- Scopes: `internal/memory/scopes.go:56-76,94,132,200,265,323,402`
- Sweeper: `internal/memory/sweeper.go:37-76` (deletion-based eviction; `manual`/pinned exempt)
- Diary: `internal/memory/agent_diary.go:13,50,68`
- Evals: `internal/evals/evals.go:42-64,140,279` · `docs/howto/evals.md`
- Council: `internal/council/council.go:36-38,84-110,115-120`
- Hooks: `internal/router/bl356_exit_hooks.go:65` · `internal/mcp/bl356_exit_hooks.go:25`
- Injection: `internal/autonomous/security.go:118` · `internal/autonomous/manager.go:142` · `internal/config/config.go:1532-1537`
- Guardrails: `internal/autonomous/guardrail_registry.go:30-42` · `internal/skills/manifest.go:66-67`
- Routing: `internal/router/bl220_comm_commands.go:307` (BL331)
- Session template: `templates/session-CLAUDE.md:55-75,117-125`
- Memory workflow: `docs/howto/automata-memory-workflow.md`
