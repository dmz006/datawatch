# Harness Research — Practitioner-Pass Synthesis (v2)

**Date:** 2026-09-19 · **Status:** Draft
**Scope of this summary:** the Sept 17-19 practitioner pass — `candidates.md` (63 raw candidates), `case-studies.md` (17 curated builders), `usage-patterns.md` (12 recurring patterns), `datawatch-mapping.md` (12 patterns mapped against the v8.19.8 baseline), `enhancements-v2.md` (9 proposals, 3-tier sequencing).
**Not restated here:** the Sept 7-11 framework pass (`synthesis.md`, `enhancement-proposals.md` #1-5, `feature-themes.md` #1-6). Where a v2 item depends on a Sept 7-11 claim, this file points to it instead of re-proposing.

---

## 1. The 5 most common usage patterns in the 17-builder cohort

Ranked by how many of the 17 builders independently arrived at the technique (source: `usage-patterns.md`). Three patterns hit 5 builders; the 4-builder band contains three patterns — patterns 4 and 5 below are shown, pattern 6 (verifier inner-loop) ties at 4.

| Rank | Pattern (builders) | Case studies behind it |
|---|---|---|
| 1 | **Memory as context compression with scope + promote** (5) | #1 Geoffrey Huntley (Ralph: AGENT.md self-update); #4 Akhil (progress.txt → AGENTS.md); #6 Niptao (Linear ticket as scoped memory); #7 Storer (Juggler typed-tree context ops); #10 joacod (nano-harness memory workflow) |
| 2 | **Human-in-loop approval gate** (5) | #6 Niptao (`go/skip` gate); #14 albert-ying (`autolab_editorial` accept/revise/reject); #7 Storer (per-action tool allow/deny); #11 pcollins (per-action delegation); #16 mvschwarz (operator reads owner + checker outputs) |
| 3 | **Cost-based routing / budget caps** (5) | #2 ralphctl (cost-tiered presets, cheap generator / top-tier evaluator); #5 aattaran DeepClaude (80/20 routine/frontier, mid-session backend switch); #13 McCabe (local-first Ollama); #14 albert-ying (subscription CLIs, $0 marginal); #9 Eve (local persona + cloud only for real work) |
| 4 | **Session lineage + snapshot/rollback** (4) | #2 ralphctl (crash-resume from persisted state); #14 albert-ying (`autolab_resume` for 24h runs); #16 mvschwarz (`rig down --snapshot` → `rig up` restore); #17 Denicola (push-everything as crash net) |
| 5 | **Guardrail chains at the model boundary** (4) | #8 Verma (mandatory tool contracts + dual end-gate); #3 xr0am (tiered tools, optional container sandbox); #15 Quorum (VRAM-aware sequential Ollama scheduling); #6 Niptao (injection guardrail over all in-band content) |

Tied at 4 (pattern 6, not shown above for rank): **verifier inner-loop** — #2 ralphctl, #16 mvschwarz, #12 Featherbench, #13 McCabe. Notably, the 5-builder and 4-builder bands are exactly the axis on which `datawatch-mapping.md` finds 6 of 7 MATCH ratings — the cohort converged on the same loop datawatch already owns.

---

## 2. The 3 highest-ranked gaps (from `datawatch-mapping.md`)

Ordered by practitioner frequency (how many pattern rows / builders touch the same delta):

1. **No per-LLM-call request/response artifact** (patterns 8, 3, 12). The sole **MISSING** rating. datawatch records what the operator did (BL9), what the daemon observed (telemetry, eBPF envelopes), and what memory did (WAL) — but no surface can answer "what was the 7th prompt in session X, verbatim, and what came back?" This is the write-side primitive that `enhancement-proposals.md` #3 (lineage query + OpenInference export) *assumes but never lands*, and the input the route-on-quality and mid-run-swap gaps both need to even detect their trigger.
2. **Cost and quality not wired into routing decisions** (patterns 3, 7, 11). BL6 measures spend, BL20 routes by keyword/config order, ComputeNode failover is static priority — `routing_rules_test` exists, `routing_rules_optimize` does not. This is `feature-themes.md` #6 verbatim; no Sept 7-11 proposal claimed it, which is why `enhancements-v2.md` promotes it to a concrete proposal with a specified input contract.
3. **No mid-run model swap** (pattern 7). Registry failover makes a *downed* model survivable and per-task overrides make the *next* task cheaper — but a running tmux session cannot have its backend flipped (DeepClaude's `/_proxy/mode`, Eve's mid-conversation escalation, ralphctl's stall ladder have no datawatch grain). Composes with gap 1: the stall-triggered escalation needs the per-call artifact to detect the stall.

(For completeness, the remaining three ranked gaps are: per-action human gating delegated not owned; warn-only injection depth; and the claimed-not-open suite×backend comparison surface — `enhancement-proposals.md` #1/#2 already own it.)

---

## 3. Top-3 recommended enhancements (from `enhancements-v2.md`) — with sequencing rationale

`enhancements-v2.md` sequences nine proposals in three tiers; the top three in its net order are:

1. **`prop-v2-per-call-span-primitive`** (M complexity, H impact). One persisted span per model call on the inference adapters — assembled prompt, tool schemas, messages, output blocks, token/cache use, stop-reason — with write-time secret redaction. *Sequencing rationale:* it is the only hard prerequisite in the set — `enhancement-proposals.md` #3, `prop-v2-route-on-measured-signal`, and `prop-v2-mid-run-model-ladder` all read it, and the honest cost/latency column of `enhancement-proposals.md` #1 needs it. Pure reuse of the adapter path already in `internal/inference/`.
2. **`prop-v2-diff-scoped-gates-recoverable-critique`** (S complexity, H impact). Two upgrades to existing machinery: BL367 gates scoped by `pathPrefix` against the actual diff (baseline + re-run only touched gates), and the BL24 verifier's failure reshaped into a *recoverable* structured critique ("what went wrong + what to do next") so `auto_fix` retries are teaching signals, not blind relaunches. *Sequencing rationale:* cheapest move with the widest blast radius — BL367 runs on every autonomous PRD — and it is the critique-carrier that the tier-2 model-ladder proposal depends on.
3. **`prop-v2-route-on-measured-signal`** (M complexity, H impact) — first of the tier-2 design spikes. The steady-state tier of model selection: a `preference: cheapest-passing` term in the LLM-registry/BL20/ComputeNode walk, fed by sweep results + BL367/BL117 verdict history + BL6 cost, with a regression-detection term so a silently-degraded cheap model is demoted on data, not vibes. *Sequencing rationale:* it is `feature-themes.md` #6 promoted from theme to concrete proposal, it cannot ship as a one-liner (the signal contract, regression band, and "cheapest-first-passing" semantics are design decisions), and it consumes exactly the data layer proposals 1-2 create. The remaining tier-2 spikes (`prop-v2-mid-run-model-ladder`, `prop-v2-planning-prompt-invariants`) and tier-3 items (`prop-v2-rubric-judge-bias-controls`, `prop-v2-eval-drift-feedback`, `prop-v2-editorial-approve-gate`, `prop-v2-council-debate-method`) land after, per `enhancements-v2.md`'s "build data first, design spikes second, small standalone surfaces last" logic.

---

## 4. How this relates to the Sept 2026 framework catalog

The two passes are complementary by construction: the framework pass (Sept 7-11, 16 public vendor harnesses — promptfoo, lm-evaluation-harness, DeepEval, RAGAS, DSPy, LangGraph, AutoGen, LiteLLM, Haystack, Dify, Phoenix, Langfuse, OpenInference, Guardrails AI, pydantic-ai, Haystack/Dify) catalogs **what the surface looks like**; the practitioner pass (Sept 17-19, 17 individual builders) catalogs **what the loop looks like**. Reconciliation:

### Where they agree

- **Evals as first-class decision input.** Framework side: promptfoo / lm-eval-harness / DeepEval define sweep + scoring as the core surface. Practitioner side: Featherbench and McCabe independently arrived at the single-variable pinned-panel discipline ("any score movement is provably the model's"). Both converge on datawatch BL259, and both agree the missing piece is *comparison* (sweep matrix, #1/#2), not the runner.
- **Guardrails enforced at the model boundary.** Framework side: Guardrails AI / LiteLLM validator pipelines. Practitioner side: Verma's tool contracts, xr0am's tiered tools, Quorum's VRAM-aware scheduling, Niptao's injection flagging. Both say: the model is allowed to try, the harness blocks — plea is not policy. datawatch-mapping rates this a MATCH (guardrail library, BL369, BL366, F10 containers).
- **DAG/dependency orchestration.** Framework side: LangGraph / AutoGen / DSPy graphs. Practitioner side: ralphctl's dependency-ordered waves, xr0am's `safe-to-start` lock. datawatch-mapping: MATCH (BL117 graph + BL357 atomic claim).
- **Routing/cost as a first-class axis.** Framework side: LiteLLM's whole premise. Practitioner side: ralphctl/DeepClaude/McCabe/Eve/albert-ying. PARTIAL in both — static routing yes, feedback term no.
- **Session lifecycle as the differentiator.** Framework pass: synthesis.md names it datawatch's strongest differentiator vs all 16 catalog entries (they assume the app owns state). Practitioner pass: the 4-builder lineage/snapshot pattern (ralphctl, albert-ying, mvschwarz, Denicola) converged on the same need *independently* — a long autonomous run must be resumable, and the typed checkpoint is the escape hatch. This is the single strongest cross-pass signal: two research methodologies that share zero inputs agreed on the same axis.

### Where they disagree

- **Rank order of the observability gap.** Framework pass rates provenance/OTel as high-impact *design spike*, sequenced after eval orchestration (synthesis #2, impact-matrix top-right). Practitioner pass promotes the per-call artifact to **rank-1 gap** (`datawatch-mapping.md` gap 1): three practitioner case studies (Storer, Verma, joacod) treat the exact-call record as a *daily debugging primitive*, not an export feature. The framework pass's "read-side assembly, no new collection" premise is exactly what falls over — datawatch has no per-call rows to assemble (datawatch-mapping, MISSING).
- **What "guardrails" means in practice.** Framework pass scopes the augmentation as *validator-pipeline depth* (red-team, #5). Practitioner pass shows the same builders also gate *the loop itself* — approval gates at task boundaries (#2 pattern, 5 builders) and per-action tool authorization — surfaces the framework catalog (which models guardrails as input/output filters on a single call) does not name at all.
- **What drops out of scope.** RAG/grounding metrics (synthesis #3, theme 5) survive in the framework pass as "slip in when BL274 gains traction" — the practitioner pass has **no** corresponding pattern (no builder built a documentation-grounding loop), suggesting it is an operator-surface need, not an autonomous-loop need.
- **What is *not* contested.** No practitioner builder did what promptfoo does as its primary job (multi-provider eval sweeps), and no vendor harness owns what datawatch owns end-to-end (spawn→kill session lifecycle). The catalog and the cohort occupy disjoint parts of the same surface.

### What the practitioner loop exposes that the framework catalog did not

1. **The write-side per-call span primitive.** The catalog's observability trio (Phoenix, Langfuse, OpenInference) is an *export* story — they assume calls are already recorded. Storer/Verma/joacod show the practitioners' gap is one step earlier: *recording*. The framework pass's proposal #3 assumed the primitive; the practitioner pass lands it (`prop-v2-per-call-span-primitive`). This is the single gap the framework catalog's six categories all silently stepped over.
2. **Verifier-feedback → prompt-mutation (the "signs" edge).** Huntley/Akhil-style harnesses mutate their own planning prompt in response to observed misbehavior ("don't assume it's not implemented"), folding the failure into permanent memory. No vendor harness in the catalog does this — their evals score and stop; the practitioner loop *learns*. Framework pass: absent entirely. Practitioner pass: the concrete proposal `prop-v2-planning-prompt-invariants` (case #1 "What datawatch could learn" edge).
3. **Conditional/reactive model switching.** Framework side (LiteLLM routing) is a static config; practitioner side (ralphctl's stall ladder, Eve's auto-router, DeepClaude's mid-session flip) is *state-triggered* — the model rung is a function of the run's current condition, not the task's class alone. The framework catalog models *who* answers; the practitioner loop models *when to swap who*.
4. **Per-task editorial approval as a protocol step.** Niptao's `go/skip`, albert-ying's `autolab_editorial` + AI-fallback — a human decision that *gates execution*, not a dashboard afterthought. The framework pass's HITL surface is approval *UX* (PRD-level approve/reject), which the mapping rates PARTIAL precisely because the practitioner granularity (per-task, three-way, with delegated fallback) is not in the catalog's model of what "approval" is.
5. **The harness as a seam that is deliberately swappable.** Featherbench's and McCabe's single-variable doctrine ("the harness is the seam — swap in the model when it's released, re-run the identical golden set") is a *practitioner* insight about what a harness must not be (a load-bearing assumption about the model). The framework catalog treats the model as an input to score; the practitioner pass treats it as a variable to control. The framework catalog had no entry demanding this discipline.

---

## 5. Net

The two passes are not in tension; they are the same research at two resolutions. The framework pass tells you the *surface* (eval sweep, OTel export, grounding scores, validator depth, routing) — all valid, all already sequenced in `enhancement-proposals.md` #1-5. The practitioner pass tells you the *loop* (memory as compression, approval as protocol, cost as first-class, lineage as escape hatch, verification as a second LLM) — and it re-ranks the surface work: the per-call span is a prerequisite, not a design spike; the routing feedback term is the strategic win, not a theme; and three edges (signs, editorial gate, reactive switch) that the catalog never names are now concrete proposals in `enhancements-v2.md`.

*No code was written for this pass. One file created: `docs/plans/harness-research/synthesis-v2.md`. The 5 most common patterns, the 3 highest-ranked gaps, the top-3 enhancements with sequencing rationale, and the reconciliation of the Sept 7-11 framework pass against the Sept 17-19 practitioner pass — all grounded in the same-week body of work, differentiated against `synthesis.md`/`enhancement-proposals.md` rather than re-stating it.*
