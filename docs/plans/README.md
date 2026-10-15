# Roadmap

datawatch is a Go daemon that runs autonomous AI coding sessions, called
Automata (internally PRDs), across several LLM backends. The v10.0.0 release
is the point where the remaining backlog is built by datawatch's own Automata
engine instead of by hand. "Pre-v10.0.0" means the work that has to land first,
in the order below, so that engine can be trusted with the rest.

Shipped work is summarized in [HISTORY.md](HISTORY.md). Status below was
checked against the live PRD registry on 2026-10-15. Each row links to its
design doc where one exists.

## Active order

Items run in this order, one at a time against this tree, until the write lock
(BL428 Phase 2) ships. Until then, sequencing is enforced by hand, not by the
daemon.

### Stage A: execution engine reliability

| # | Item | Status | Depends on |
|---|---|---|---|
| 1 | BL441 PRD-completion content auto-fix never fires | in progress (PRD `34bd5877`, running) | - |
| 2 | BL452 "The V Method" — logprob-classification guardrail backend (design) | in progress (PRD `468db26e`, awaiting review) | - |

### Stage B: the Automaton execution engine

| # | Item | Status | Depends on |
|---|---|---|---|
| 3 | [BL427 Phase 2](2026-10-13-prd-lifecycle-events.md) Built-in lifecycle-event sinks | in progress (PRD `5154c783`, awaiting review) | BL427 Phase 1 (shipped) |
| 4 | [BL428 Phase 2](2026-10-14-concurrency-and-budget-design.md) Project-tree write lock | not started (PRD `29bd62a1`, draft) | BL428 Phase 1 (shipped) |
| 5 | [BL431 Phase 1](2026-10-14-automation-orchestration-design.md) Automation data model, store and CRUD | not started (PRD `2e7ebad7`, draft) | - |
| 6 | [BL431 Phase 2](2026-10-14-automation-orchestration-design.md) Automation coordinator with auto-advance | not started | 3, 4, 5 |
| 7 | [BL427 Phase 3](2026-10-13-prd-lifecycle-events.md) Persisted subscriptions and CRUD across surfaces | not started | 3 |
| 8 | [BL428 Phase 3](2026-10-14-concurrency-and-budget-design.md) Inference-cap gap closure | not started | 4 |
| 9 | [BL431 Phase 3](2026-10-14-automation-orchestration-design.md) Control operations and mid-flight editing | not started | 6 |
| 10 | BL422 Claude model and effort tiering eval | blocked (PRD `f6a38c29` failed; resume needed) | - |
| 11 | [BL431 Phase 4](2026-10-14-automation-orchestration-design.md) Model-tier defaults | not started | 10 |
| 12 | [BL428 Phase 4](2026-10-14-concurrency-and-budget-design.md) Reactive usage back-off | not started | 8 |
| 13 | [BL428 Phase 5](2026-10-14-concurrency-and-budget-design.md) Dollar-window budget | not started | 12 |
| 14 | [BL431 Phase 5](2026-10-14-automation-orchestration-design.md) BudgetPolicy | not started | 13 |
| 15 | [BL428 Phase 6](2026-10-14-concurrency-and-budget-design.md) Visibility, audit and parity close-out | not started | 13 |
| 16 | [BL431 Phase 6](2026-10-14-automation-orchestration-design.md) Automation PWA | not started | 14 |
| 17 | [BL427 Phase 4](2026-10-13-prd-lifecycle-events.md) Pending-events tool and PWA subscription UI | not started | 7 |
| 18 | [BL431 Phase 7](2026-10-14-automation-orchestration-design.md) Remaining parity, migration and close-out | not started | 16 |
| 19 | [BL427 Phase 5](2026-10-13-prd-lifecycle-events.md) Webhook sink and federation forwarding | not started | 17 |

### Stage C: remaining backlog, queued as an Automation

Once BL431 Phase 2 ships, this list is queued as an Automation and the daemon
advances it without hand sequencing.

| # | Item | Status | Depends on |
|---|---|---|---|
| 20 | [BL402](2026-10-09-gh204-result-panel.md) Result panel for scheduled jobs and integrations | not started | - |
| 21 | [BL437](2026-10-14-recurring-model-eval-design.md) Recurring hardware-aware model and effort eval | blocked (PRD `fea73c82` failed; design done) | 10, 11 |
| 22 | BL414 PRD cost rollup and token usage guardrails | not started (needs scoping against BL405) | BL405 |
| 23 | [BL405](2026-10-10-llm-enhancements.md) LLM enhancements, phases 0-10 | not started (design complete) | 6 |
| 24 | Security remediation: BL365, BL395, BL408, BL398 ([BL398 detail](2026-10-08-bl398-trivyignore-cve-review.md)) | not started (no PRDs yet) | - |
| 25 | BL412 Self-config permission-gate generalization | not started (needs design) | - |
| 26 | BL413 Diff-aware project-rules enforcement | not started (needs design) | - |
| 27 | BL411 Pre-v10.0.0 dead-code audit | not started | 20-26 |
| 28 | BL409 File and function naming audit | not started | 27 |
| 29 | BL424 Operator integration guide | not started (PRD `0e2ac398` approved, held to last) | 28 |

## Separate lanes

These do not block the sequence above.

| Item | Status |
|---|---|
| BL444 MCP SSE listener retries bind at boot | not started |
| BL445 Review and merge external MCP `public_url` PR (GH#208) | not started |
| BL446 Federation as a sibling-agent mail transport | not started |
| BL453 Can open models decompose PRDs? (research) | not started (PRD `d2f87f0c`, draft) |

## Excluded from v10.0.0

- BL415 `dependency-review` required check. Deferred until after v10.0.0 by decision.

## Open items outside the active order

These are not in the locked order. Their live status is recorded here so they
are not lost.

| Item | Status |
|---|---|
| [BL433](2026-10-14-pwa-performance-audit.md) PWA performance audit under load | blocked (PRD `049f4713`) |
| BL434 Splash animation on every full-screen refresh | approved, not started (PRD `88fdf815`) |
| BL435 Retry with already-correct output must not fail on empty diff | approved, not started (PRD `191d0c95`) |
| BL436 Per-turn chunked output capture (design) | approved, not started (PRD `01bcf5cb`) |
| BL438 Research: Hermes Agent | approved, not started (PRD `6952fceb`) |
| BL439 Research: datawatch as infrastructure for Hermes Agent | approved, not started (PRD `2712d303`) |
| BL442 PWA service worker force-navigates controlled windows | needs review (PRD `39f7af28`) |
| BL443 goose and aider inference adapters | not started (a clear error for `llm_test` shipped in v9.0.88) |
| BL448 Voice transcription times out on long recordings | draft (PRD `6bded76a`) |
| BL450 Startup-dialog auto-dismiss for one-shot tasks | draft (PRD `8272882d`) |
| [BL400](2026-10-09-alerts-conditions-and-filtering.md) Alerts: conditions, filtering, SIEM parity | deferred, open questions |
| BL393 Nested tags for Automata and PRDs | deferred |
| BL392 Cross-repo GitHub configuration hardening audit | mostly done |
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
