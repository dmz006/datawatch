# Roadmap

datawatch is a Go daemon that runs autonomous AI coding sessions, called
Automata (internally PRDs), across several LLM backends. The v10.0.0 release
is the point where the remaining backlog is built by datawatch's own Automata
engine instead of by hand. "Pre-v10.0.0" means the work that has to land first,
in the order below, so that engine can be trusted with the rest.

Shipped work is summarized in [HISTORY.md](HISTORY.md). Status below was
checked against the live PRD registry on 2026-10-17. Each row links to its
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
| 1 | BL441 PRD-completion content auto-fix never fires | blocked (PRD `34bd5877`) | - |
| 2 | [BL452](2026-10-15-logprob-classification-guardrail-design.md) "The V Method" — classification-based guardrail verdicts via Ollama 0.35+ native decision endpoint or logprobs (design pass) | failed (design pass; PRD `468db26e`); needs a new PRD | - |
| 3 | BL455 Fix B126: a retry can record the wrong pre-task SHA, producing a false "no output" failure on real, correctly-committed work | failed (PRD `2713dbb9`); retry PRD `6b99f00f` is in needs_review. This is the next critical-path item: BL431 Phase 1 (row 9) waits on it | - |
| 4 | **BL462** A running PRD task's session must not be impacted by a daemon restart/update | not started (PRD `c96f2104`, draft) — **operator priority**, blocks safely updating the daemon during active work | - |
| 5 | **BL463** MCP connection reliability, session isolation clarity, error-mode parity with REST | not started (PRD `bf78e50a`, draft) — every spawned session depends on MCP as its primary interface | - |
| 6 | **BL464** Execute/build session names need a function prefix (`autonomous:task:` to match `autonomous:verify:`/`autonomous:decompose`) | not started (PRD `dc6c597b`, draft; the earlier `e303d88d` was cancelled) — small, real observability fix | - |

### Stage B: the Automaton execution engine

| # | Item | Status | Depends on |
|---|---|---|---|
| 7 | [BL427 Phase 2](2026-10-13-prd-lifecycle-events.md) Built-in lifecycle-event sinks | shipped in v9.3.0 (PRD `172bf30f`, completed; `5154c783` is a stale draft). Checklist gaps remain: testing-tracker row, operations docs, datawatch-app issue, live smoke | BL427 Phase 1 (shipped) |
| 8 | [BL428 Phase 2](2026-10-14-concurrency-and-budget-design.md) Project-tree write lock | blocked (PRD `29bd62a1`; tasks 1 to 4 done, task 5 failed three times on the verifier running the wrong model). Resumes once the verifier fix (PRD `9d3033b8`) is merged and installed. Config keys landed in the 9.4.0 development build and the same-tree admission check in the 9.5.0 development build, neither released | BL428 Phase 1 (shipped) |
| 9 | [BL431 Phase 1](2026-10-14-automation-orchestration-design.md) Automation data model, store and CRUD | running (PRD `b9bd3f99`; all done except the live smoke and the final test suite, the smoke is stuck verifying, probably the same verifier bug) | - |
| 9a | Corrective closure before the coordinator (operator order, 2026-10-18): finish the open phase-completion gaps of the unreleased work since v9.0.87, so the first release after it is complete. Items: the verifier model fix (PRD `9d3033b8`, patch); lifecycle event sinks (tracker row, operations docs, datawatch-app issue, live smoke); the 9.5.0 one-shot work (live smoke, git opt-in, `*-prompt` one-shots, commit guard); then the first complete release, numbered 9.6.0 (operator: leave the gap, do not renumber 9.1 to 9.5) | in progress (verifier fix running on the Claude thread) | 8, 9 |
| 10 | [BL431 Phase 2](2026-10-14-automation-orchestration-design.md) Automation coordinator with auto-advance (now also scoped as a real quality/regression gate — test-coverage validation, cross-cutting-surface regression checks, stall/idle alerting, dynamic reprioritization for urgent bugs; see design doc open question 17) | not started | 7, 8, 9, 9a |
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
| 27 | BL435 Retry with already-correct output must not fail on empty diff | blocked (PRD `191d0c95`) | - |
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
| 49 | BL468 pi backend echoes `DATAWATCH_COMPLETE` even when pi exits non-zero, so a failed pi run can look complete | not started (found 2026-10-16 while debugging decompose; not blocking now — pull up only if it recurs and holds up PRDs) | - |
| 50 | BL469 Worktree mode is the default for all PRDs and for the datawatch project; direct tree mode stays as an explicit opt-in (`git.direct`); same-workdir admission check so two PRDs never run on one direct tree | in progress: admission check and `worktree_default` switch and `git.direct` and `git.source_repo` fields written (uncommitted, builds); worktree gate rewrite in `ensureWorktreeIfConfigured` not yet done | BL428 Phase 2 tree lock |
| 51 | BL470 PRD commits stage only the task's declared `write_dirs`; a worktree-mode PRD scopes to its own worktree; a direct-mode PRD with no `write_dirs` does not auto-commit (operator-confirmed 2026-10-17) | not started | BL469 |
| 52 | BL471 Worktree pruning: archive vs purge of finished worktrees | deferred to automation PRD planning, after 10.0.0 (arch/project/release) is done; do not build before then | BL469 |
| 53 | BL472 Usage and cost calculator: model the session-local meter and the shared weekly ceiling from operator-reported snapshots (see [concurrency and budget](2026-10-14-concurrency-and-budget-design.md) section 4b) plus inferred token and cost rollups | not started | BL414 |
| 54 | BL473 Build the Automation coordinator to the [supervisor rules and lessons](2026-10-17-supervisor-rules-and-lessons.md): plan-validation gate, failure triage, verifier self-check, tree admission, after-done checklist, registry-versus-README consistency, silent-state alerting, push + tag each completed minor version and watch its CI run until green | not started (rules documented 2026-10-17; belongs inside BL431 Phase 2) | 10 |
| 55 | BL474 Hand-started `*-prompt` sessions (for example `datawatch session new --backend pi-prompt`) are recorded as interactive (`one_shot` unset), so the validation gate, the finished-session reaper and retention do not cover them; they sit in `waiting_input` after the worker exits. Treat every `*-prompt` backend as a one-shot | not started (found 2026-10-17 on session `216a`) | - |
| 56 | BL475 Commit enforcement: a worker can run `git add`/`git commit` itself despite the instruction not to (commit `17e6658f` was made by the pi worker, not the harness), which bypasses the validation gate. Add a per-session commit guard so only the harness commits, after validation. Also never auto-commit a directory that is not a project: the completion commit tried `git add -A` in `/home/dmz` 44 times (all failed, exit 128, nothing committed, no remote) | not started (found 2026-10-17; 9.5.0's gate blocks the `/home/dmz` case for sessions without `--validate`) | BL474 |
| 57 | BL476 Git is opt-in, not required. Since v9.2.0 every one-shot session gets git: `git init` of its directory when it is not a repo, a snapshot, and a completion commit, and the `session.auto_git_commit` opt-out was retired. Scripts and scheduled jobs (for example a mail-rules job) are not development work and must never touch git. Only PRD/development sessions, or a session that asks for it, use git. 9.5.0 already stops the commit for sessions without `--validate`; `git init` still runs for any one-shot | not started (operator principle 2026-10-17; fix first in the BL474/475 batch) | - |
| 58 | BL477 Verifier session timeout is a setting, not a hard-coded 10 minutes (`autonomous.verification_timeout_seconds`, default 1800) | in progress (PRD `0a9abdb5`, Claude lane) | 9a |
| 59 | BL478 A finished decompose must store its stories or fail with the reason (twice a 25 to 38 KB plan was dropped and the PRD ended with zero stories, no error) | draft PRD `f4de5138` | 9a |
| 60 | BL479 Planning model follows the decompose backend; bound the respawn of a decompose session that stops at a model error; do not resume a decompose for a cancelled PRD | draft PRD `eb922590` | 9a |
| 61 | BL480 `set_llm` must leave omitted fields unchanged (it clears them today), on every surface | draft PRD `e65daa74` | 9a |
| 62 | BL481 Compact MCP and CLI replies for PRD tools (reset_task replies reach 12 MB) with a detail level on `prd_get` | draft PRD `0caefd05` | 9a |
| 63 | BL482 Task and story level LLM overrides allowed on a blocked PRD, with the allowed states a setting (`autonomous.task_override_statuses`) | draft PRD `516a7777` | 9a |
| 64 | BL483 Automation policy: every option for how automation runs is configurable per project (release rules, cadence, lane limits, backends and models, retry and escalation limits, plan checks, override-lock states, git default, timeouts, defect intake, escalation channel), with our tested behaviour as the shipped default policy; belongs inside BL431 Phase 2 and BL473 | not started | 10 |
| 65 | Fixed in the unreleased development build (listed so the release notes of 9.6.0 can include them): verifier used the wrong model (v9.5.1, PRD `9d3033b8`); an empty-diff first attempt passed as already landed (v9.5.2, PRD `1e79466d`) | done, not released | - |

## Opportunistic, non-blocking research

Internal research — does not block Stage A/B/C, can run alongside it.

| Item | Status |
|---|---|
| BL453 Can open models decompose PRDs? (research) | not started (PRD `19f9a693`, draft; covers decompose reliability, execution-scope, and a positive data point — 3 research PRDs fully open-model decomposed+executed successfully) |
| [BL465](2026-10-16-deterministic-git-commit-design.md) Move the primary git commit out of the worker's own tool loop | **shipped v9.2.0** — built via pi-prompt (PRD `137ed868`), merged to main; live smoke run against a sandbox daemon still pending |
| [BL466](2026-10-16-pi-opencode-evaluation.md) pi LLM backend for open-model PRD execution | **shipped v9.1.21** — full backend + config/CLI/PWA/comm parity built tonight (reprioritized from v11.0.0 research to active, per the "no claude-code PRD executors until Sunday" constraint); real multi-task PRD pilot through datawatch's own session manager still pending |
| [BL467](2026-10-16-mcp-bridge-registration-race.md) MCP channel bridge registration is first-come, not owner-bound (design) | needs_review (design only, no PRD yet); not urgent, nothing currently broken |

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

- Operating rules: [supervisor rules and lessons learned](2026-10-17-supervisor-rules-and-lessons.md)
- Engine and budget: [automation orchestration](2026-10-14-automation-orchestration-design.md),
  [lifecycle events](2026-10-13-prd-lifecycle-events.md),
  [concurrency and budget](2026-10-14-concurrency-and-budget-design.md)
- Model evaluation: [recurring model eval](2026-10-14-recurring-model-eval-design.md),
  [open-model matrix](2026-10-15-bl449-open-model-matrix.md)
- Guardrails and rules: [logprob-classification guardrail (BL452)](2026-10-15-logprob-classification-guardrail-design.md),
  [tooled deterministic guardrails](2026-10-12-tooled-deterministic-guardrails.md),
  [project-rules enforcement](2026-10-10-configurable-project-rules-enforcement.md)
- Other: [LLM enhancements](2026-10-10-llm-enhancements.md),
  [plugin extension surfaces](2026-10-09-plugin-extension-surfaces.md),
  [PRD git workflow](2026-10-10-prd-git-workflow.md),
  [PWA performance audit](2026-10-14-pwa-performance-audit.md)
