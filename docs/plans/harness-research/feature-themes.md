# Harness Research — Candidate Enhancement Themes

**Date:** 2026-09-07 · **Status:** Draft · **Companion:** `synthesis.md`, `impact-matrix.md`, `methodology.md`

*Sources:* `synthesis.md` (including the re-verified catalog reference set; `examples-catalog.md` itself was not committed by the prior catalog session) and `methodology.md` category definitions. Each theme below is an enhancement candidate — where user need exceeds DataWatch's current coverage — with the catalog entries that express that need.

---

## 1. Eval harness integration & suite × backend sweeps

- **User needs**
  - Run the same behavioral suite against multiple LLMs / ComputeNodes in one invocation, with a side-by-side (pass rate, latency, tokens, cost) comparison — the promptfoo / lm-evaluation-harness UX.
  - Capability tiers vs. regression tiers (e.g. ~70% capability gate vs. ~99% regression gate) instead of a single flat pass/fail.
  - Eval gates *inside* orchestration: "do not run downstream PRD B until suite X passes on the artifact PRD A produced" — the DAG-verification gap vs. LangGraph/DSPy.
  - Model-upgrade and model-selection decisions answered by data, not by hand-running the suite per backend and eyeballing two `Run` rows.
- **DataWatch value**
  - Cheapest, largest-gap win in the set: BL259 already has YAML suites, threshold gates, per-case results; BL117 already has DAG nodes + verdict records; a `sweep` verb (suite × backend matrix, `parent_run_id` children) plus an `eval` node kind reuses ~all existing machinery.
  - Closes the one category DataWatch is missing vs. catalog peers, and makes the eval verdict a first-class citizen of the PRD pipeline alongside guardrails.

## 2. Data drift detection + AI feedback loops

- **User needs**
  - Notice when model outputs, retrieval quality, or pass rates degrade over time (model updates, memory bloat, prompt drift) — DeepEval's "continuous eval" and RAGAS's regression-metric posture.
  - Close the loop: a failing suite should feed back into the system that can fix it (prompt, persona, memory entry, AGENT.md rule) rather than stopping at a red dash.
  - Trend visibility: eval pass rate / cost / latency as time series per suite, per backend, per project — Langfuse-style dashboards.
  - Alerts when a suite that used to pass at 100% regresses below threshold on a routine sweep.
- **DataWatch value**
  - Persisted eval runs + BL6 cost counters give the longitudinal data already; a drift layer = trend queries over `Run` history plus S14b alert rules fired on eval regression.
  - The PRD-scan → fix-sub-PRD → AGENT.md rule-proposal loop (SAST/secrets/deps) is the exact feedback-loop shape to generalize: eval-failure → auto-fix task → rule/prompt correction, all reusing the guardrail verdict stream and autonomous loop.
  - Differentiator: most catalog eval frameworks score and stop; only DataWatch owns the agent session lifecycle needed to act on the score.

## 3. Data provenance tracing + OpenInference/OTel interop

- **User needs**
  - A lineage query: "given this output in session S, which memory rows, model calls, and upstream session outputs fed it?" — the core Phoenix/Langfuse/OpenInference value proposition.
  - See DataWatch agent sessions as standard traces in the operator's existing observability stack, not a bespoke UI.
  - Replay-from-trace: restart a decision, or branch from a prior state, using the recorded span model (LangGraph checkpointing's closest analogue).
- **DataWatch value**
  - Provenance fragments already exist and are the seed of the span model: memory breadcrumbs (`scope_promote`), session parent/child (BL347/BL351), per-task telemetry, memory WAL.
  - High strategic impact if done as a design spike first: a read-side span assembly (no re-collection) + a gated OTLP/OpenInference exporter would make DataWatch the only catalog peer that unifies OS-level envelopes, session lineage, and memory provenance into one queryable trace.
  - Feasibility is the risk (new span schema, query path, exporter), hence spike-before-build per synthesis.

## 4. Guardrail depth: red-team validator pipeline at the model boundary

- **User needs**
  - Block (not warn-only) prompt injection entering autonomous worker specs — the BL369 gap; Guardrails AI's validator pipelines and promptfoo's vulnerability scanner express this as the standard.
  - Multi-stage validation: lexical/regex layer → LLM adversarial classifier (in-bounds / out-of-bounds / exfiltration / tool-abuse) with confidence thresholds.
  - Per-stage action escalation with an auditable verdict trail, tunable per-PRD via existing override priority.
- **DataWatch value**
  - Small surface with high security impact: BL369's hook point, guardrail verdict writer, plugin dispatch, and the per-PRD/per-task override priority are all reuse; the new work is the classifier stage + a gated `block` mode.
  - Stage verdicts join the existing guardrail verdict stream, so orchestrator verdict UX and `block_on_injection` (which already anticipates this gate) light up with no new UX.
  - Diminishing-returns caveat from the impact matrix: core surface is already covered; depth matters mainly as the autonomous surface grows.

## 5. RAG / grounding metrics for MCP-served answers

- **User needs**
  - A measured quality signal on `docs_search` results (BL274): answer-relevance / faithfulness per excerpt, not just rank order.
  - Citation-precision checking on LLM-generated plans (`docs_apply`) so low-grounding plans get flagged or re-planned.
  - A threshold filter so callers can stop over-trusting rank-1 on ambiguous queries.
- **DataWatch value**
  - Embedder vectors for the index already exist — rescore is a dot-product pass per result; a 0–1 grounding score attaches to the existing response shape with no new subsystem.
  - Makes "trust in MCP-served answers" measurable, which strengthens the docs-as-MCP surface (trust tiers, plan-then-execute approval in Sprint 3) and gives RAGAS-style grounding a native home even though the catalog gap here is narrower.
  - Sequenced last per analysis: cheap, low strategic blast radius, slips in when BL274 gains traction.

## 6. Quality- and cost-aware model routing

- **User needs**
  - Route work to backends on measured quality and cost, not just keyword rules or config order — LiteLLM-style load balancing with actual evaluation data.
  - Budget governance with live cost visibility (BL6 exists but feeds no routing decision).
  - Failover chains that prefer the cheapest *passing* model for a task class, and detect when the cheap option has silently degraded.
- **DataWatch value**
  - LLM registry (v7 S2) + ComputeNode registry + ordered failover + BL6 cost tracking + BL20 routing rules + BL30 cooldowns already compose 80% of this theme; what's missing is the feedback term.
  - Feeds back into themes 1 and 2: sweep results (suite × backend) and drift trends become the inputs a routing rule can consume — `routing_rules_test` becomes `routing_rules_optimize`.
  - No new subsystem required; the value is closing the loop between measurement (already built) and selection (still static).

---

## Sequencing read (cross-referenced with `impact-matrix.md` and `enhancement-proposals.md`)

1. Theme 1 (eval sweep + DAG eval nodes) — do now; reuses BL259/BL117 almost entirely.
2. Theme 4 (red-team validator pipeline) — small surface, closes the security gap with existing guardrail plumbing.
3. Theme 3 (provenance/OTel) — design spike on the span model before any build.
4. Theme 2 (drift + feedback loops) — mostly read-side trend queries over already-persisted runs once theme 1 lands; generalizes the PRD-scan fix loop.
5. Theme 6 (quality-aware routing) — composes the measurement layer from themes 1–2; no new machinery.
6. Theme 5 (grounding metrics) — cheapest theme, slottable anytime BL274 gains traction.
