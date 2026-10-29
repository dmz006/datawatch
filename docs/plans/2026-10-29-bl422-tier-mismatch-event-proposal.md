# BL422 follow-up: tier-mismatch lifecycle event proposal

**Date:** 2026-10-29
**Version:** none (planning document; plan-document commits are exempt from the version bump)
**Tracker:** registry row 182 (with row 14, BL422 tiering eval, and BL437 recurring eval design)

Confirmed: no — no option is chosen. This file records the problem and the options; nothing is decided and no implementation PRD is filed until a `Confirmed: yes` line replaces this one.

## Problem

The operator's 2026-10-23 decision (lifecycle question 9) is that a tier-mismatch event will probably be one more opt-in event kind, added only after BL422's results exist and agree with the design. The event has never been defined beyond one sentence, BL422 has produced no results, and its PRD is parked. Three questions follow: what the event is, what data it needs, and whether to unblock the eval that feeds it.

## What exists today (verified)

| Piece | State | Proof |
|---|---|---|
| Event dispatcher with opt-in kinds | EXISTS. `scope_warning` and `guardrail_warn` are the two opt-in warning kinds, off by default, informational only, never block work | `internal/autonomous/notify_events.go` (`OptInNotifyEvents`), `lifecycle_emit_warning.go` |
| Event payload | EXISTS. `LifecycleEvent` carries ids, `Details`, `Files`, `Guardrail`, `Link`, `Decision`, `Verdict` | `lifecycle_event.go` |
| Resolved execution backend, model and source level per task | EXISTS (task, story, PRD, Automation, global, escalation) | `resolveTaskExecutionLLM` in `executor.go` |
| Resolved verifier backend, model, effort and source per verification | EXISTS, recorded on the verdict | `GuardrailVerdict.VerifierBackend/Model/Effort/Source`, `models.go` |
| Per-task effort field | EXISTS (`Task.Effort`, plus story and PRD levels). The effort fall-through is in `executeOne` | `models.go`, `executor.go` |
| Eval fixtures and drivers | EXISTS. 4 task types by 13 execution tiers, plus 13 decompose cells, Claude only, per-cell binary verifiers | `testdata/eval/model-tiering/`, `scripts/eval-model-tiering.sh`, `scripts/eval_model_tiering.py` |
| Eval results | MISSING. No `docs/testing/model-tiering-eval.md`, no results checked in | `docs/testing` has no tiering file |
| A "recommended tier" anywhere in config or code | MISSING. A search for tier-policy, recommended-tier names found nothing | `internal/` search |
| A task-type field matching the eval's types (analysis, scripting, docs, review) | MISSING. `Task.Kind` is only `smoke`, `final_gate` or empty; `PRD.Kind` is feature, bug or other | `models.go` |
| Verification-tier test | MISSING by design: needs a guarded global config flip, no per-task override | fixtures README |
| BL422 PRD `f6a38c29` | failed, then blocked and parked until after 10.0 | registry rows 14 and 228 |
| Recurring eval (BL437) | design only, not built. It says BL422 results stand unless a new Claude model id appears. Proposed Claude spend cap is a $25 per run proposal, not measured | `2026-10-14-recurring-model-eval-design.md` |

## The event, if built

Kind `prd.tier_mismatch` (the lifecycle design's name; existing kinds use no common prefix, so the name is open). Fires at task start, when the resolved backend, model and effort differ from the recommended tier for that task. Payload: PRD, story and task ids, touchpoint (execution, decompose or verification), resolved backend/model/effort, the level that supplied them, the recommended tier and its source. Informational only, off by default, delivered through the existing notify sinks. Two inputs do not exist yet: a recommended-tier table, and a way to classify a task into a task type.

## Options

**A. Keep waiting (park until after 10.0).** Builds nothing. Cost: 0 PRDs. Risk: the event stays undefined and the eval stays failed; the 10.0 arc carries an open row.

**B. Resume the full BL422 eval as designed, then define the event from its results.** Builds: results doc, a recommended-tier table, then a follow-up PRD for the event. Cost: one eval resume (52 execution plus 13 decompose cells of paid Claude usage, size not measured) plus one event PRD of about 4 to 6 tasks (kind, emit at task start, config table, PWA/MCP/REST/CLI wiring, tests, howto). Risk: the PRD already failed once and the reason is not recorded in the repo; paid usage; results may not map to the task-type problem.

**C. Resume a narrowed eval (fewer tiers or task types), then define the event.** Same shape as B with a smaller grid. Cost: lower paid usage, same event PRD. Risk: a narrow result may not support a policy for all task types, which is the condition the operator set for adding the event.

**D. Build the event now against an operator-defined tier table, independent of BL422.** The table is empty by default and the operator fills it; BL422 results later become the suggested contents. Cost: one PRD of about 4 to 6 tasks. Risk: it overrides the 2026-10-23 decision to wait for results; the task-type classification problem is still open.

## Recommendation

Option C, with the event design held until the narrowed results exist. It honours the operator's wait-for-results condition, gets real data at a bounded cost, and avoids building a table nobody can fill yet. Before resuming, record why `f6a38c29` failed so the retry does not repeat it.

## Open questions for the operator

1. Which option: A, B, C or D?
2. If B or C: set a paid-usage ceiling for the eval run (yes to the $25 proposal from the recurring eval design, or give a number)?
3. If C: which task types and tiers stay in the narrowed grid (propose: all 4 task types, the 3 Claude models currently in use, effort low and high only)?
4. How should a task be classified into a task type: operator tag per task, a decompose-time label, or inferred by the daemon?
5. Should the verification tier be in scope (needs the guarded global config flip), yes or no?

## Dependencies and order

Needs: the reason `f6a38c29` failed (not recorded in the repo; unverified), usage headroom, and BL431's Automation-level model default as the place results are applied. The event follows the results; the recurring eval (BL437) follows both. Eval resume is post-10.0 per row 228 unless the operator moves it.

## Parity surface

| Surface | Change a build would make |
|---|---|
| Config | event kind in `notify_events` valid list; tier table key (name not chosen) |
| REST | tier table read/write; kind appears in event listings |
| MCP | notify-events tools accept the new kind; tier table tool |
| CLI | same kind in notify-events commands |
| Comm channel | notification text via the existing describe function; tier table verbs |
| PWA | kind in the notify-events picker and the event list; tier table editor |
| Mobile | parity issue on the datawatch-app repo for the picker and list |
