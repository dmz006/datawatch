# Harness Research — Impact/Effort Matrix

**Date:** 2026-09-07 · **Status:** Draft · **Companion:** `synthesis.md`

Axes: **effort** (low → high, X) and **strategic impact** (low → high, Y).
Entries come from `synthesis.md` → "Where augmentation is possible".

|          | **Low effort** | **High effort** |
|----------|----------------|-----------------|
| **High impact** | **1. Eval orchestration** — sweep verb (suite × backend) on `eval_run` + eval node kind in `orchestrator_graph`. Reuses BL259 + BL117 machinery almost entirely, so weeks not months. High impact: it closes the quality loop (eval-as-guardrail inside DAGs, multi-LLM side-by-side, capability/regression tiers) — the biggest differentiator gap vs. promptfoo/lm-eval-harness. | **2. Data provenance tracing** — unified lineage query ("which memory rows / models / upstream sessions produced this output") + OpenInference/OTel span export + replay-from-trace. High impact: turns DataWatch's scattered provenance fragments (memory breadcrumbs, lineage, per-task telemetry) into a first-class, stack-integrated observability story no catalog peer offers. But it demands a new span model over existing data, an exporter, and a query engine — a design spike before build. |
| **Low impact** | **3. RAG / grounding metrics** — RAGAS/DeepEval-style retrieval + answer-relevance scorers attached to `docs_search` (BL274). Low effort: a few scorers over existing recall results, no new subsystem. Low impact: `docs_search` is a side surface; measured grounding scores there don't touch the dominant eval/orchestration loop and have no catalog peer to compete with. | **4. Red-teaming depth** — promptfoo-style vulnerability-scanner + Guardrails-style validator pipeline layered over BL369. High effort: full attack-catalog harness, validator pipeline, and CI integration. Low strategic impact here: BL369 injection guard, PRD-scan SAST/secrets/deps loop, and the guardrail library already cover the core surface; depth beyond that has diminishing returns vs. #1–#2. |

## Reading the matrix

- **Do now (top-left):** #1 eval orchestration — cheapest path to the largest capability gap; also makes #2's spans more valuable later because DAG-guardrail verdicts already have a home.
- **Do next, scoped (top-right):** #2 provenance — start as a design spike (span model over existing session/telemetry data, no re-collection) per the synthesis recommendation.
- **Defer / opportunistic (bottom-left):** #3 grounding metrics — small enough to slip in if `docs_search` gains traction; not worth a dedicated track.
- **Decline for now (bottom-right):** #4 red-team depth — revisit only if a threat-model requirement appears; current guards are adequate for the operator-facing surface.
