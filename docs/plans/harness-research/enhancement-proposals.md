# Harness Research — Enhancement Proposals

**Date:** 2026-09-07 · **Status:** Draft · **Companion:** `synthesis.md`, `methodology.md`

Selected from the four augmentation themes in `synthesis.md` (#1 eval orchestration, #2 data provenance, #3 RAG grounding metrics, #4 red-teaming depth). Five proposals; each rated on feasibility (how little new machinery is required vs. what BL259/BL117/BL369/BL274 already provide) and impact (breadth of sessions/PRDs affected).

---

## 1. Eval Sweep — suite × backend matrix

**Problem.** `eval_run` executes one suite against a single backend and emits a single pass/fail. Choosing between LLMs/ComputeNodes (or comparing a before/after prompt) requires hand-running the suite once per backend and eyeballing two `Run` rows — no side-by-side, no aggregate comparison.

**Proposed feature.** Add a sweep verb to the evals surface: `eval_run` accepts a matrix (suite × list of LLM refs, optionally × model overrides) and fan-outs the suite per cell in parallel under the existing max-parallel plumbing. A new `RunSet` object stores per-cell results with normalized metrics (pass_rate, latency, tokens, cost via BL6) and renders a side-by-side comparison table, including pass-vs-threshold per cell.

**User benefit.** Model selection and model-upgrade decisions become one command with an apples-to-apples table instead of manual bookkeeping; regressions introduced by a new model surface immediately across every suite.

**Implementation sketch.** Extend the BL259 runner with a loop over the backend matrix that reuses the per-suite executor verbatim, recording each result as a child of a new parent run row (new `parent_run_id` field, no new store). Comparison rendering is a read-side aggregation over the children, plus an MCP/CLI verb (`eval_run --sweep`) that returns the table.

**Feasibility: 5/5 · Impact: 4/5**

---

## 2. Eval nodes in the PRD-DAG orchestrator

**Problem.** BL117 graphs run PRD + guardrail nodes; there is no eval-node type. A suite gate today only exists if the operator separately wires quality gates (BL367) or runs evals out-of-band — nothing in the DAG can say "do not run downstream PRD B until suite X passes against the artifact produced by PRD A".

**Proposed feature.** Add an `eval` node kind to the orchestrator graph planner and runner: it executes a named suite (optionally with sweep params from proposal 1) against the project directory state, records a verdict record exactly like a guardrail verdict, and its pass/fail blocks dependent nodes. Existing `orchestrator_verdicts` and cancellation/verdict UX are reused unchanged.

**User benefit.** Acceptance criteria become first-class DAG gates: multi-PRD features ship only when the full behavioral suite passes, turning ad-hoc "run the evals" memory into enforced ordering.

**Implementation sketch.** Introduce an `eval` node kind in the graph planner that resolves to the BL259 runner behind the same `Verdict` interface guardrail nodes use (runner interface, new adapter). The runner's dependency-walk and verdict-appending code needs no structural change — only the node-type dispatch and a suite-name field on the node row.

**Feasibility: 5/5 · Impact: 4/5**

---

## 3. Unified lineage query + OpenInference span export

**Problem.** Provenance fragments exist — memory breadcrumbs (`scope_promote`), session parent/child (BL347/BL351), per-task telemetry, memory WAL — but there is no query that answers "which memory rows, model calls, and upstream session outputs fed this output?" and no OTel/OpenInference export, so traces cannot feed existing observability stacks (Phoenix/Langfuse) or support replay.

**Proposed feature.** A lineage query surface: given a session + output anchor (or a task in a PRD), walk the span graph (agent spans → LLM-call spans → memory-read spans → upstream-session spawn spans) and return the contributor list with role, timestamp, and content excerpts. Optionally serialize the same graph to OpenInference-shaped spans (OTLP batch export to any collector), letting external tools render DataWatch sessions as standard traces.

**User benefit.** Debugging a bad autonomous output becomes a query instead of archaeology across four stores; the same session becomes visible in the operator's existing OTel observability without a bespoke UI.

**Implementation sketch.** Define the span schema over existing rows (session id, task id, memory row id, compute-node/model, parent span id) and build it as read-side assembly — no new collection path, since the breadcrumbs already exist on the memory side. Export is a mapping function to OTLP + a background flusher, gated behind a config flag so nothing sends unless the operator sets a collector endpoint.

**Feasibility: 3/5 · Impact: 5/5**

---

## 4. Grounding metrics for docs_search / docs_read

**Problem.** BL274 serves ranked excerpts with source/path/anchor but has no measured quality signal: no answer-relevance or faithfulness score, no citation-precision check. Operators cannot tell whether the top hit actually supports the question, and LLM-driven `docs_apply` plans are unconstrained by retrieval quality.

**Proposed feature.** Add per-result grounding scores to docs search results: a lightweight scorer (cross-encoder rerank or embedder-similarity delta) that produces a 0–1 relevance/grounding score per excerpt, a threshold filter in `docs_search`, and an aggregate "grounding report" (top-k coverage, citation precision) attached to `docs_apply` plans. Thresholds surface in the same config surface as BL274 trust config.

**User benefit.** Trust in MCP-served answers becomes measurable — low-grounding plans can be flagged for re-planning, and `docs_search` callers can stop over-trusting rank-1 on ambiguous queries.

**Implementation sketch.** Reuse the BL274 index embedder for query-vs-excerpt rescoring (already computed vectors, one dot-product pass per result) and attach scores to the existing response shape. `docs_apply` gains a scoring step that summarizes per-step citation support and attaches it to the plan object; the LLM-translated-path long-tail can later adopt the same scorer via the plan-then-execute approval flow (BL274 Sprint 3).

**Feasibility: 4/5 · Impact: 3/5**

---

## 5. Red-team validator pipeline (deepening BL369)

**Problem.** BL369's injection guard is a warn-only phrase scan applied to PRD/task specs; it is a lexical match, not a validator pipeline. promptfoo-style vulnerability scanning is missing, so a sufficiently obfuscated or task-shaped injection still flows into an autonomous worker unchecked.

**Proposed feature.** Elevate the guard to a multi-stage validator pipeline at the PRD/task boundary: (1) the existing regex layer, (2) a cheap LLM-based adversarial classifier (classify: benign / instructing-out-of-bounds / exfiltration / tool-abuse) with a confidence threshold, and (3) per-stage action escalation — warn (today's default) or block (new, gated and audited). Stage verdicts join the guardrail verdict stream so they appear in `per_automaton_guardrails` and orchestrator verdicts, and the classifier stage can run as a PRD-scan-loop sibling (SAST/secrets/deps pattern) with automatic fix-sub-PRD generation for false-positives reported into rules.

**User benefit.** Autonomous loops get real pre-execution injection defense rather than a warning, and block actions leave an auditable verdict trail the operator can tune per-PRD via existing override priority.

**Implementation sketch.** Implement the pipeline as a new guardrail library entry that the BL369 hook already invokes, with stages registered in the same plugin/guardrail dispatch (regex classifier, then optional LLM classifier reusing the ask path). Verdict emission reuses the existing guardrail verdict writer; "block" mode short-circuits PRD/task creation behind the same `mcp.allow_self_config`/audit gate that `block_on_injection` already anticipates.

**Feasibility: 4/5 · Impact: 5/5**

---

## Sequencing (per synthesis.md recommendation)

1. **Proposal 1 + 2 first** — both reuse BL259/BL117 with minimal new machinery; together they close the largest gap (#1).
2. **Proposal 5 second** — small surface, closes the security gap (#4) with existing guardrail plumbing.
3. **Proposal 3 as a design spike** — scope the span model before any export work (synthesis recommendation).
4. **Proposal 4 last** — depends on the embedder being stable and the BL274 plan-then-execute gate (Sprint 3) landing.
