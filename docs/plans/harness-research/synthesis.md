# Harness Research — Executive Synthesis (Close-Out)

**Date:** 2026-09-30 (close-out of the full research set) · **Status:** Final
**Cohort:** 20 individual builders in `case-studies.md` (target was ≥15, ideally 20+ — the count is met: **20 builders**, curated down from 63 raw candidates in `candidates.md`, 4 aggregator lists excluded).
**Inputs reconciled here:** the framework pass (Sept 7-11: `methodology.md`, `examples-catalog.md`, `feature-themes.md`, `impact-matrix.md`) and the practitioner pass (Sept 17-30: `candidates.md`, `case-studies.md`, `usage-patterns.md`, `datawatch-mapping.md`, `synthesis-v2.md` exec-summary, `enhancements-v2.md`, plus the current re-grounded proposals in `enhancement-proposals.md`).
**Companions:** `diagrams.md` (pattern→package and P1 data-flow diagrams), `README.md` (directory index + reading order).

---

## 1. The 5 most common builder patterns (with the cases behind them)

Ranked by how many of the 20 builders independently arrived at the pattern (`usage-patterns.md`, 19 recurring patterns + single-builder appendix):

| # | Pattern (slug) | Builders | Cases behind it |
|---|---|---|---|
| 1 | Durable on-disk state, disposable context (10) | `durable-state-continuity` | #1 Huntley (specs/ + fix_plan.md), #2 ralphctl (persisted sprint/branch), #4 Akhil (prd.json + progress.txt), #6 Niptao (Linear as queue+memory), #16 OpenRig (topology snapshot), #19 pie, #20 Forcefield |
| 2 | Behaviour as versioned files (10) | `behaviour-as-files` | #1 (PROMPT.md/AGENT.md), #6 (one skill file + 180-line bridge), #7 Storer (JS extensions), #9 Eve (112 agents/111 commands/273 skills, each a markdown file), #10 joacod, #11 pcollins, #14, #16, #17 |
| 3 | Isolated context + workspace per task (9) | `isolated-agent-context` | #2 (per-task git worktrees), #6 (worktree per ticket), #7 (child threads), #3 (parallel agents + Docker), #17 (worktree + tailnet), #16 (pods), #14, #19 |
| 4 | Human-in-the-loop approval gate (9) | `human-approval-gate` | #6 (`go/skip` reply gate), #7/#11/#20 (per-tool allow/deny), #10 (recorded policy decision), #14 (accept/revise/reject), #19 (`/inbox claim`), #17 (PR-review gate) |
| 5 | Bounded loops + cost accounting (8) | `bounded-loops-cost-accounting` | #2 (maxAttempts → blocked, never done), #5 DeepClaude (`/_proxy/cost`, ~17x cheaper), #9 (40-round cap), #7 (per-transaction use), #13 (token counts that exposed a wiring bug), #19/#20 (caps + honest accounting) |
| — | Just below the top, still load-bearing | — | Tiered model routing (7, incl. #1 oracle-on-error, #2 stall ladder, #9 auto-router), recorded run artifacts (7, incl. #7/#10/#12/#13) — the one datawatch does **not** fully own |

Read: patterns 1-5 are the *shape* of every serious individual harness; the 7-builder pattern "recorded, inspectable run artifacts" is the clearest point where the cohort and datawatch's actual surface diverge (see gaps).

## 2. The 3 most important datawatch gaps

From the ranked gaps in `datawatch-mapping.md` (v8.37.4, pattern-level coverage: 12 MATCH / 7 PARTIAL / 0 fully MISSING by surface, one primitive MISSING entirely):

1. **No per-LLM-call request/response artifact** (ranked gap #1, the only MISSING). `internal/inference/dispatcher.go:37-59` ships `Request`/`Response` as ephemeral transport types; nothing persists them. 7 builders converged on exactly this need (#7 "open the exact assembled request", #8 ref-registry audit trail, #10 "every provider call is a first-class event", #13 "identical token counts" bug-detector, #12 raw-JSONL publication). The route-on-quality, eval-sweep cost column, lineage export, and stall-detection all implicitly need this primitive first.
2. **Cost/quality not wired into routing; no stall-triggered model ladder** (ranked gap #2). BL6 measures spend as observation only; `internal/autonomous/executor.go` retries the *same* model on stall. The cohort's #2 ralphctl ("climb the ladder one rung on a stall, carrying the critique") and #5 DeepClaude (80/20 live switch) both implement exactly the missing feedback term. This is `feature-themes.md` theme 6 verbatim.
3. **No mid-run model swap** (ranked gap #3). `session/manager.go` Restart kills and relaunches — it loses the session's own context and token cache, which is the entire point of the DeepClaude pattern in a tmux-session architecture. A live-session LLM rebind (keep the pane, flip the *next* call) is the missing grain.

(Secondary, smaller: per-action tool gate delegated to the harness behind datawatch's container boundary, injection guard lexical-only — gap #4; VRAM-contention scheduling absent from the capacity ledger — gap #5.)

## 3. Top 3 recommended enhancements — and why them first

From `enhancement-proposals.md` (v2, 19-pattern re-grounding), in the sequenced recommendation's own order:

1. **P1 — per-LLM-call span artifact.** First because it is the one gap no prior proposal claimed (MISSING, not PARTIAL), it has the largest anchor (7 builders on `recorded-run-artifacts` plus 4 more citing it in deltas), and it is the load-bearing dependency of P2's stall signal, P3's swap audit, and the deferred lineage/OpenInference export. Cheapest to build correctly: one append-only store on a choke point (the dispatcher already serializes every call) + three read verbs, modeled on the existing memory-WAL shape. Everything else is strictly cheaper if it lands first.
2. **P4 — eval sweep + eval DAG gate.** Second (not because it's higher-impact, but because it's S-effort by its own terms and its *practical reach is 10 builders*: `single-variable-eval` + `independent-verifier` + `harness-failure-tuning` all cite it as their measurement tool). It does not depend on P1 — it re-aggregates BL6's existing per-session counters — and it produces the single decision artifact (suite × backend table with cost column) that P2's cost-feedback rung selection and every earlier routing hand-wave needs. This is the framework pass's original proposals #1+#2, promoted and re-grounded.
3. **P2 — stall-triggered model-ladder escalation.** Third because it closes the second-ranked gap (the `feature-themes.md` theme 6 that has sat unclaimed across two research passes), it turns the operator's manual 80/20 hand-tuning across the multi-LLM registry into an automatic one, and it drops from L-to-M once P1's span tail gives it an honest stall signal and P4's sweep table gives the operator proof that a rung change is worth making.

Full seven-proposal sequence and the three explicit declines (mid-session auto-tune, per-action veto API, premature lineage export) are in `enhancement-proposals.md`; do not re-propose from the framework pass without re-reading its Declines section.

## 4. Cross-pass reconciliation (one paragraph)

The two passes share no inputs (methodology.md for selection, not content), yet they converge on the same two axes, independently: (a) **session lifecycle / durable state** — the framework pass calls it datawatch's strongest differentiator vs all 16 vendor harnesses (they assume the app owns their own state); the practitioner pass shows 10 of 20 builders independently building it, and 9 building isolated per-task context. (b) **Provenance / per-call observability** — the framework pass's #2 gap (no OTel/OpenInference-shaped lineage) and the practitioner pass's gap #1 (no per-LLM-call artifact) are the same gap described from opposite ends, and both converge on the same fix shape: a span primitive over data datawatch already owns, not a new tracing stack. The one category neither pass's cohort covers is RAG/grounding — zero individual builders in the 20 touch retrieval — which means that gap (framework #3) can only be filled by adopting a vendor pattern (RAGAS-style claim-decompose → LLM-judge), not by copying a practitioner's loop.

---

*Documentation only — no code was written in the close-out pass. Files produced/updated this pass: `synthesis.md` (this file, replaced from its framework-pass content), `diagrams.md` (new), `README.md` (rewritten as the directory index). Prior framework-pass content remains in `synthesis-v2.md` lineage docs and in git.*
