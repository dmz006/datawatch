# harness-research — Index for the directory

**Root:** `docs/plans/harness-research/`
**Two research passes** share this directory. The **Sept 7-11 framework pass** catalogs 16 public vendor harnesses and derives the baseline gap analysis. The **Sept 17-19 practitioner pass** studies 17 individual builders (Ralph-loop lineage, solo OSS harnesses, eval rigs) and derives the v2 gap analysis and v2 enhancements. The two passes share no inputs and are reconciled against each other in `synthesis-v2.md`.

Reading order is bottom-up in the "Recommended reading order" section at the end. Do not read them top-to-bottom if you are short on time — the synthesis files are the load-bearing documents.

---

## Sept 7-11 framework pass

| File | One-line description |
|---|---|
| `methodology.md` | Define the inclusion criteria (public, functional, documented, harness-shaped), the 6 target categories (eval, RAG, guardrail, orchestration, verification, session/state), and the commercial-adjacent appendix scope. This is the "why these 16" — read it before `examples-catalog.md` if you are questioning a selection. |
| `examples-catalog.md` | 16 public vendor harnesses (promptfoo, lm-eval-harness, DeepEval, RAGAS, DSPy, LangGraph, AutoGen, pydantic-ai, LiteLLM, Haystack, Dify, Phoenix, Langfuse, OpenInference, Guardrails AI), each with stack, data flow, license/stars, and a "vs. DataWatch" comparison row. Stars and repo URLs re-verified via GitHub API on 2026-09-10. |
| `synthesis.md` | The framework pass's 5-category gap analysis (eval orchestration, provenance/OTel, RAG grounding, red-teaming) plus the "Where DataWatch already matches" table — the table that establishes session-lifecycle ownership as datawatch's differentiator. |
| `feature-themes.md` | The 6 candidate enhancement themes the gap analysis produces: (1) eval sweep + DAG eval nodes, (2) drift + feedback loops, (3) provenance/OTel, (4) red-team validator pipeline, (5) RAG grounding, (6) quality- and cost-aware routing. Each theme is "user needs" + "datawatch value," not a concrete proposal. |
| `impact-matrix.md` | The 4 quadrants (effort × strategic impact) the 6 themes map into, with the sequenced read: #1 do now (top-left), #2 design spike (top-right), #3/4 deferred or declined (bottom row). |
| `enhancement-proposals.md` | 5 concrete proposals (eval sweep, DAG eval node, lineage query + OpenInference export, grounding metrics, red-team pipeline) with feasibility/impact scores and a 4-step sequencing. This is the framework pass's recommendation; the v2 pass differentiates against these 5. |

## Sept 17-19 practitioner pass

| File | One-line description |
|---|---|
| `candidates.md` | Over-collection pass: 63 raw candidates (individuals, solo builders, personal projects) who built their own AI agent harnesses, coding loops, eval rigs, guardrails, or orchestration tooling. HTTP status spot-checked 2026-09-17. |
| `case-studies.md` | 17 curated builders (from 63 candidates) selected on real-person / harness-shaped / documented criteria. Each case study is a concrete recurring usage loop a builder independently arrived at. |
| `usage-patterns.md` | The 12 recurring patterns that 2+ builders independently arrived at (ranked by builder count), plus a one-off techniques appendix. This is the cross-builder abstraction — the "what the loop looks like" layer. |
| `datawatch-mapping.md` | Maps the 12 patterns against the v8.19.8 baseline: MATCH / PARTIAL / MISSING per pattern. Identifies the 3 highest-ranked gaps and differentiates them against the Sept 7-11 proposals. |
| `enhancements-v2.md` | 9 new enhancement proposals grounded in the practitioner pass, differentiated (not re-stated) against the 5 framework proposals. Includes a 3-tier sequencing ("build data first, design spikes second, small standalone surfaces last") and a traceability diagram (pattern slug → datawatch module → v2 proposal slug). |
| `synthesis-v2.md` | Exec-summary of the practitioner pass: the 5 most common patterns with case-study names, the 3 highest-ranked gaps, the top-3 recommended enhancements with sequencing rationale, and the reconciliation of the framework pass against the practitioner pass. |
| `README.md` | This file: the index for the directory, grouped by pass, with the recommended reading order. |

## Recommended reading order

Top to bottom, shortest-to-longest first (start at the load-bearing docs, then zoom into evidence):

1. `synthesis-v2.md` — the v2 exec-summary (practitioner pass); read this first.
2. `datawatch-mapping.md` — the MATCH/PARTIAL/MISSING table against the v8.19.8 baseline.
3. `enhancements-v2.md` — the 9 v2 proposals with sequencing and traceability.
4. `synthesis.md` — the framework pass's 5-category gap analysis (context for what the v2 differentiates against).
5. `enhancement-proposals.md` — the 5 framework-era proposals the v2 pass references and extends.
6. `usage-patterns.md` — the 12 recurring patterns the v2 pass draws from.
7. `case-studies.md` — the 17 builder case studies behind the patterns.
8. `feature-themes.md` — the 6 candidate themes the framework pass's gap analysis produces.
9. `impact-matrix.md` — the framework pass's sequencing logic.
10. `examples-catalog.md` — the 16 vendor harnesses the framework pass cataloged.
11. `methodology.md` — the inclusion criteria (read only if you are auditing a selection).
12. `candidates.md` — the 63 raw candidates behind the 17 case studies.

---

*No code was written for this pass. One file created: `docs/plans/harness-research/README.md`. Index for the directory, grouped into Sept 7-11 (framework) and Sept 17-19 (practitioner) passes, with a recommended reading order. Under 120 lines as per the task. The two deliverables of this task are `synthesis-v2.md` and `README.md`; no other file in the directory was modified.*
