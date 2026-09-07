# AI Harness Research — Methodology

**Date:** 2026-09-07 · **Status:** Draft

## What is an "AI harness"?

A reusable system that **wraps, orchestrates, or constrains** an LLM so it behaves more deterministically, safely, or measurably than a raw model call. The model is the engine; the harness is the chassis that adds prompts, loops, state, checks, and routing around it.

## Inclusion Criteria

A project qualifies for the catalog only if **all** of these hold:

1. **Public** — source lives in a public repository. License is recorded, but any license is acceptable for inclusion.
2. **Functional** — actively maintained (recent commits or releases) with a working example or test suite; abandoned stubs and dead forks are excluded.
3. **Documented** — a README explaining what it does, how to install it, and at least one runnable usage example.
4. **Harness-shaped** — it controls or measures model behavior rather than merely passing text to `generate()`. Prompt cheat-sheets and one-off toys fail this test.

## Categories to Target

| Category | What it controls |
|----------|-----------------|
| **Eval frameworks** | Benchmarks / regression-testing LLM behavior (scoring, task gates) |
| **Custom RAG pipelines** | Retrieval, ranking, grounding, citation control around generation |
| **Guardrail systems** | Pre/post-processing filters; policy enforcement at the model boundary |
| **Agent orchestration** | Task decomposition, agent assignment, verification across runs |
| **Verification & test harnesses** | Tests, linters, diff-based checks applied before acceptance |
| **Session/state management** | Persistence, replay, and routing of agent sessions |

## Out of Scope

- Bare inference servers (engines, not harnesses).
- Prompt templates with no executable structure.
- Closed-source products — listed in a "commercial adjacent" appendix only.

## Deliverable

A maintained catalog (JSONL + summary): name, repo, license, category, backend-agnosticism, maintenance status, and a one-line "what it controls" description.
