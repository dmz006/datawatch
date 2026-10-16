# Roadmap

datawatch is a Go daemon that runs autonomous AI coding sessions, called
Automata (internally PRDs), across several LLM backends. The v10.0.0 release
is the point where the remaining backlog is built by datawatch's own Automata
engine instead of by hand. "Pre-v10.0.0" means the work that has to land first,
in the order below, so that engine can be trusted with the rest.

Shipped work is summarized in [HISTORY.md](HISTORY.md). Status below was
checked against the live PRD registry on 2026-10-15. Each row links to its
design doc where one exists.

## Rules

All operating rules live in [`AGENT.md`](../../AGENT.md) — this file holds
project-tracking data only. If an operating rule ends up duplicated here,
move it to AGENT.md and leave a cross-reference instead.

**Folder hygiene** (operator-directed 2026-05-05): `docs/plans/` only holds
dated plan docs (`YYYY-MM-DD-*.md`) ≤ 7 days old and release notes for the
current minor version line. Everything older auto-archives to
`historical-plans/` / `historical-releasenotes/`, enforced by
`scripts/tidy-plans.sh`. `scripts/release-smoke.sh` runs
`tidy-plans.sh --check` first and **aborts the release** if the folder
needs tidying — run `scripts/tidy-plans.sh` (no flags) to do the moves,
then commit before re-running smoke.

## Active order

Items run in this order, one at a time against this tree, until the write lock
(BL428 Phase 2) ships. Until then, sequencing is enforced by hand, not by the
daemon.

**Federation-aware shared-resource coordination** (additive scope inside
BL428/BL431/BL405, not a separate item): detecting a shared compute node or
billing account across federated peers via keyed-hash fingerprint exchange,
operator confirmation before merging capacity pools, and live cross-daemon
lease/back-off. Lands as BL428 §3c (row 8) and §4e (rows 12-13) in
[concurrency-and-budget-design.md](2026-10-14-concurrency-and-budget-design.md),
BL431's open question 14, and BL405 Phase 7.

### Stage A: execution engine reliability

| # | Item | Status | Depends on |
|---|---|---|---|
| 1 | BL441 PRD-completion content auto-fix never fires | in progress (PRD `34bd5877`, running) | - |
| 2 | BL452 "The V Method" — logprob-classification guardrail backend (design) | running (PRD `468db26e`; main tree, docs-only — accepted collision risk alongside BL441 since BL428 Phase 2's write lock doesn't exist yet) | - |
| 3 | BL455 Fix B126: a retry can record the wrong pre-task SHA, producing a false "no output" failure on real, correctly-committed work | in progress (PRD `2713dbb9`, decomposing; worktree-isolated, local GPU) | - |
| 4 | **BL462** A running PRD task's session must not be impacted by a daemon restart/update | not started (PRD `c96f2104`, draft) — **operator priority**, blocks safely updating the daemon during active work | - |
| 5 | **BL463** MCP connection reliability, session isolation clarity, error-mode parity with REST | not started (PRD `bf78e50a`, draft) — every spawned session depends on MCP as its primary interface | - |
| 6 | **BL464** Execute/build session names need a function prefix (`autonomous:task:` to match `autonomous:verify:`/`autonomous:decompose`) | not started (PRD `e303d88d`, draft) — small, real observability fix | - |

### Stage B: the Automaton execution engine

| # | Item | Status | Depends on |
|---|---|---|---|
| 7 | [BL427 Phase 2](2026-10-13-prd-lifecycle-events.md) Built-in lifecycle-event sinks | in progress (PRD `5154c783`, awaiting review) | BL427 Phase 1 (shipped) |
| 8 | [BL428 Phase 2](2026-10-14-concurrency-and-budget-design.md) Project-tree write lock | not started (PRD `29bd62a1`, draft) | BL428 Phase 1 (shipped) |
| 9 | [BL431 Phase 1](2026-10-14-automation-orchestration-design.md) Automation data model, store and CRUD | blocked (PRD `b9bd3f99`; both tasks failed, same "no output" symptom as BL455 — pending that investigation) | - |
| 10 | [BL431 Phase 2](2026-10-14-automation-orchestration-design.md) Automation coordinator with auto-advance (now also scoped as a real quality/regression gate — test-coverage validation, cross-cutting-surface regression checks, stall/idle alerting, dynamic reprioritization for urgent bugs; see design doc open question 17) | not started | 7, 8, 9 |
| 11 | [BL427 Phase 3](2026-10-13-prd-lifecycle-events.md) Persisted subscriptions and CRUD across surfaces | not started | 7 |
| 12 | [BL428 Phase 3](2026-10-14-concurrency-and-budget-design.md) Inference-cap gap closure (includes §3c federation shared-compute-node detection) | not started | 8 |
| 13 | [BL431 Phase 3](2026-10-14-automation-orchestration-design.md) Control operations and mid-flight editing | not started | 10 |
| 14 | BL422 Claude model and effort tiering eval | blocked (PRD `f6a38c29` failed; resume needed) | - |
| 15 | [BL431 Phase 4](2026-10-14-automation-orchestration-design.md) Model-tier defaults | not started | 14 |
| 16 | [BL428 Phase 4](2026-10-14-concurrency-and-budget-design.md) Reactive usage back-off | not started | 12 |
| 17 | [BL428 Phase 5](2026-10-14-concurrency-and-budget-design.md) Dollar-window budget (includes §4e federation shared-billing detection) | not started | 16 |
| 18 | [BL431 Phase 5](2026-10-14-automation-orchestration-design.md) BudgetPolicy | not started | 17 |
| 19 | [BL428 Phase 6](2026-10-14-concurrency-and-budget-design.md) Visibility, audit and parity close-out | not started | 17 |
| 20 | [BL431 Phase 6](2026-10-14-automation-orchestration-design.md) Automation PWA | not started | 18 |
| 21 | [BL427 Phase 4](2026-10-13-prd-lifecycle-events.md) Pending-events tool and PWA subscription UI | not started | 11 |
| 22 | [BL431 Phase 7](2026-10-14-automation-orchestration-design.md) Remaining parity, migration and close-out | not started | 20 |
| 23 | [BL427 Phase 5](2026-10-13-prd-lifecycle-events.md) Webhook sink and federation forwarding | not started | 21 |

### Stage C: remaining backlog, queued as an Automation

Once BL431 Phase 2 ships, this list is queued as an Automation and the daemon
advances it without hand sequencing.

| # | Item | Status | Depends on |
|---|---|---|---|
| 24 | [BL402](2026-10-09-gh204-result-panel.md) Result panel for scheduled jobs and integrations | not started | - |
| 25 | BL459 datawatch-community skills catalog — research complete, build the catalog's proposed skills | research complete (`docs/catalog/skills-catalog.md`, `skill-specs.md`); easy, high-benefit — pulled forward from v11.0.0 (PRD `275ae17c`, draft) | - |
| 26 | BL434 Splash animation on every full-screen refresh | approved, not started (PRD `88fdf815`) | - |
| 27 | BL435 Retry with already-correct output must not fail on empty diff | approved, not started (PRD `191d0c95`) | - |
| 28 | BL436 Per-turn chunked output capture (design) | approved, not started (PRD `01bcf5cb`) | - |
| 29 | BL442 PWA service worker force-navigates controlled windows | needs review (PRD `39f7af28`) | - |
| 30 | [BL433](2026-10-14-pwa-performance-audit.md) PWA performance audit under load | blocked (PRD `049f4713`) | - |
| 31 | BL392 Cross-repo GitHub configuration hardening audit | mostly done | - |
| 32 | BL443 goose and aider inference adapters | not started (clear error for `llm_test` shipped v9.0.88; adapters remain) | - |
| 33 | BL448 Voice transcription times out on long recordings | draft (PRD `6bded76a`) | - |
| 34 | BL450 Startup-dialog auto-dismiss for one-shot tasks | draft (PRD `8272882d`) | - |
| 35 | BL393 Nested tags for Automata and PRDs | not started (previously held pending explicit authorization — operator folded it back into the queue 2026-10-15) | - |
| 36 | BL461 Tag-based browsing + tile redesign for PRD/session lists at scale (design) | not started (PRD `c46f6df9`, draft) | 35 |
| 37 | [BL437](2026-10-14-recurring-model-eval-design.md) Recurring hardware-aware model and effort eval | blocked (PRD `fea73c82` failed; design done) | 14, 15 |
| 38 | BL414 PRD cost rollup and token usage guardrails | not started (needs scoping against BL405) | BL405 |
| 39 | [BL405](2026-10-10-llm-enhancements.md) LLM enhancements, phases 0-10 | not started (design complete) | 10 |
| 40 | Security remediation: BL365, BL395, BL408, BL398 ([BL398 detail](historical-plans/2026-10-08-bl398-trivyignore-cve-review.md); BL395's F-2 isolation detail: [2026-10-04-f2-session-worker-isolation.md](historical-plans/2026-10-04-f2-session-worker-isolation.md)) | **fully decided, zero open design questions — implementation only**; no PRDs yet. Real implementation order already set: Design A → C → B → D (per BL395's own master index) | - |
| 41 | BL454 Fix B125: `autonomous.*` runtime config changes (verification_backend/model and likely others) have no effect until a full daemon restart | not started (PRD `70aa7fa5`, draft) | - |
| 42 | BL412 Self-config permission-gate generalization | not started (needs design) | - |
| 43 | BL413 Diff-aware project-rules enforcement | not started (needs design) | - |
| 44 | BL411 Pre-v10.0.0 dead-code audit | not started | 24-43 |
| 45 | BL409 File and function naming audit | not started | 44 |
| 46 | BL424 Operator integration guide | not started (PRD `0e2ac398` approved, held to last) | 45 |
| 47 | BL444 MCP SSE listener retries bind at boot (external bug report) | not started | - |
| 48 | BL445 Review and merge external MCP `public_url` PR, GH#208 (external bug-fix) | not started | - |

## Opportunistic, non-blocking research

Internal research — does not block Stage A/B/C, can run alongside it.

| Item | Status |
|---|---|
| BL453 Can open models decompose PRDs? (research) | not started (PRD `19f9a693`, draft; covers decompose reliability, execution-scope, and a positive data point — 3 research PRDs fully open-model decomposed+executed successfully) |

## Excluded from v10.0.0

- BL415 `dependency-review` required check. Deferred until after v10.0.0 by decision.

## v11.0.0 roadmap (separate lanes)

Not required for v10.0.0. Organized by lane rather than a single sequence —
nothing here blocks anything in the active order above.

**Research complete, proposals pending triage:**

| Item | Status |
|---|---|
| BL457 Harness research: 20-builder survey + enhancement proposals (PRD `8baa807b`) | research complete (`docs/plans/harness-research/`); proposals (P1 span artifact, P4 eval sweep, P2 stall-ladder) pending v11.0.0 prioritization |
| BL458 openrig.dev adoption plan (PRD `ef9dcac1`) | research complete (`docs/plans/2026-09-24-openrig-adoption-plan.md`); adoption plan pending v11.0.0 prioritization |

**LLM research reconciliation:**

| Item | Status |
|---|---|
| BL460 Implement the unaddressed recommendations from the open-LLM research ([reconciliation doc](2026-10-15-llm-research-reconciliation.md)) | HOLD — file/decompose only, needs explicit operator go-ahead before running (touches live LLM routing used by every other active PRD) |

**Community/personal-agent design (held for v11.0.0 planning):**

| Item | Status |
|---|---|
| BL456 Personal agents with personality and decision-making, for a real community user | placeholder only — original attempt (PRD `51c531b6`) produced zero output; needs a real collaborative design session, not another autonomous pass |

**Feature requests and deferred research:**

| Item | Status |
|---|---|
| BL446 Federation as a sibling-agent mail transport (external feature request, not a bug) | not started |
| BL438 Research: Hermes Agent | approved, not started (PRD `6952fceb`) |
| BL439 Research: datawatch as infrastructure for Hermes Agent | approved, not started (PRD `2712d303`) |
| [BL400](2026-10-09-alerts-conditions-and-filtering.md) Alerts: conditions, filtering, SIEM parity | deferred, open questions |
| BL241 Matrix.org communication channel | deferred, needs design interview |

## Design docs

The per-topic design documents live alongside this file, named
`2026-10-DD-<topic>.md`:

- Engine and budget: [automation orchestration](2026-10-14-automation-orchestration-design.md),
  [lifecycle events](2026-10-13-prd-lifecycle-events.md),
  [concurrency and budget](2026-10-14-concurrency-and-budget-design.md)
- Model evaluation: [recurring model eval](2026-10-14-recurring-model-eval-design.md),
  [open-model matrix](2026-10-15-bl449-open-model-matrix.md)
- Guardrails and rules: [tooled deterministic guardrails](2026-10-12-tooled-deterministic-guardrails.md),
  [project-rules enforcement](2026-10-10-configurable-project-rules-enforcement.md)
- Other: [LLM enhancements](2026-10-10-llm-enhancements.md),
  [plugin extension surfaces](2026-10-09-plugin-extension-surfaces.md),
  [PRD git workflow](2026-10-10-prd-git-workflow.md),
  [PWA performance audit](2026-10-14-pwa-performance-audit.md)
