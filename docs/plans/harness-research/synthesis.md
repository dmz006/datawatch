# Harness Research — Synthesis

**Date:** 2026-09-07 · **Status:** Draft · **Companion:** `methodology.md`

*Note: `examples-catalog.md` was not committed by the prior catalog session (session stalled before writing). The reference set below was re-verified against the GitHub API on 2026-09-07; entries: promptfoo (MIT, 24.9k★), lm-evaluation-harness (MIT, 13.9k★), openai/evals (19.4k★), DeepEval (Apache-2.0, 18.2k★), RAGAS (Apache-2.0, 15.6k★), Guardrails AI (Apache-2.0, 7.4k★), DSPy (MIT, 37.8k★), LangGraph (MIT, 41.2k★), AutoGen (CC-BY-4.0, 60.9k★), pydantic-ai (MIT, 19.8k★), LiteLLM (58.2k★), Haystack (Apache-2.0, 26.4k★), Dify (154.8k★), Phoenix (ELv2, 11.4k★), Langfuse (34.3k★), OpenInference (Apache-2.0, 1.2k★).*

## Where DataWatch already matches or overlaps

| Catalog category | Representative harnesses | DataWatch surface today |
|---|---|---|
| Eval frameworks | promptfoo, lm-evaluation-harness, DeepEval, RAGAS, openai/evals | **BL259 evals** — YAML suites in `~/.datawatch/evals/`, threshold pass/fail, per-case results; **BL259 P2 v6.10.1** bridges Algorithm Mode's Measure phase into the runner; quality gates (BL367) block on regression |
| Guardrail systems | Guardrails AI, LiteLLM guardrails | Guardrail library (sast/secrets/deps scans), profiles, per-PRD/per-task override priority, plugin hooks, BL369 injection guard (warn-only), BL366 verifier diff injection cap |
| Agent orchestration | LangGraph, AutoGen, Dify, DSPy, Haystack | PRD-DAG orchestrator (BL117) with guardrail verdicts; autonomous PRD loop (BL24) with LLM planning, fan-out, verifier; Council (BL260) multi-persona debate; work queue (BL357); exit hooks (BL356) |
| Backend/routing harness | LiteLLM | LLM registry (v7 S2) + ComputeNode registry (v7 S1): kind adapters, ordered failover, per-PRD/per-task model override, cost tracking (BL6), cooldowns (BL30) |
| Observability | Phoenix, Langfuse, OpenInference | Observer federation, eBPF envelopes, telemetry/hook event capture, session timelines — but captured daemon-side + OS-level, not OTel-exported |
| Session/state management | (nearly all; LangGraph checkpointing) | tmux-backed session lifecycle, restart/rollback (BL29), parent/child lineage (BL347/BL351), schedule-spawn — **DataWatch's strongest differentiator vs all catalog entries** |
| Skills/packaging | pydantic-ai "harness" topic, Dify skill packs | Skills registry sync (BL255), skills-load MCP surface, skill-contributed guardrails |

Net: DataWatch already covers 6 of the 7 methodology categories natively, and it is the only entry in the set that owns the **session lifecycle** (spawn through kill, lineage, rollback) instead of assuming the app owns its state.

## Where augmentation is possible

1. **Evaluation orchestration.** Today an eval suite runs flat against one backend and a single pass/fail gate exists (BL367). Promptfoo/lm-eval-harness-style orchestration is missing: run the same suite across multiple LLMs/nodes with side-by-side comparison, capability-vs-regression tiers (PAI's ~70%/~99% split), and eval-as-guardrail inside DAGs (`orchestrator_graph` has no eval node type). Cheapest win: a "sweep" verb on `eval_run` (suite × backend matrix) + a DAG node kind that records the verdict like a guardrail.
2. **Data provenance tracing.** Phoenix/Langfuse/OpenInference's core value is trace-level lineage (which retrieval, model call, or prompt version produced token X). DataWatch has provenance fragments — memory breadcrumbs (`scope_promote`), session parent/child, per-task telemetry — but no unified lineage query: "given this output in session S, which memory rows, models, and upstream session outputs fed it?"; no OTel/OpenInference export, so it can't sit in existing observability stacks; no replay-from-trace path.
3. **RAG/grounding metrics.** RAGAS/DeepEval-style retrieval & answer-relevance scorers don't exist; docs_search (BL274) has no measured grounding score for cited excerpts.
4. **Red-teaming depth.** promptfoo's vulnerability-scanner and Guardrails' validator pipeline go beyond BL369's (currently warn-only) injection phrase scan and the PRD-scan loop (SAST/secrets/deps).

## Recommendation

Adopt #1 (eval orchestration) first — it reuses BL259 and BL117 machinery almost entirely — then #2 as a design spike, scoping an OpenInference-shaped span model over existing session/telemetry data rather than re-collecting.
