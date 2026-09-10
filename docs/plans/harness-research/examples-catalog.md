# Harness Research — Examples Catalog

**Date:** 2026-09-10 · **Status:** Draft · **Companion:** `methodology.md`, `synthesis.md`

16 publicly visible AI harnesses meeting the inclusion criteria from `methodology.md` (public, functional, documented, ≥1 of: eval loop, orchestration, guardrails, observability, routing). Stars and repo URLs re-verified via GitHub API on 2026-09-10.

---

## 1. promptfoo

| Field | Value |
|---|---|
| **URL** | https://github.com/promptfoo/promptfoo |
| **License / Stars** | MIT · 24.9k★ |
| **Primary function** | CLI + library for LLM output evaluation and red-teaming; runs suites of test cases across multiple providers/prompts and reports pass/fail with a diff view |
| **Stack** | TypeScript/Node.js; provider plugins for OpenAI, Anthropic, Ollama, Azure, AWS Bedrock, Vertex; YAML/JSON test configs |
| **Data flow** | Test YAML → provider adapters → LLM calls (parallel) → assertion scorers → HTML/JSON report. Providers can be swapped per test case; output is scored by built-in and custom graders. |
| **vs. DataWatch** | Most direct analog to DataWatch BL259 evals — same YAML-suite posture. DataWatch adds: algorithm-mode integration (Measure phase), per-PRD quality gates (BL367), and verifier diff–injection cap (BL366). promptfoo is broader on multi-provider sweeps and built-in adversarial scanner (red-team plugin). |

---

## 2. lm-evaluation-harness (EleutherAI)

| Field | Value |
|---|---|
| **URL** | https://github.com/EleutherAI/lm-evaluation-harness |
| **License / Stars** | MIT · 13.9k★ |
| **Primary function** | Standardized benchmark runner for language models across 60+ academic tasks (MMLU, GSM8k, HellaSwag, etc.); used as the de-facto LLM comparison harness for open-model rankings |
| **Stack** | Python; HuggingFace Transformers / PEFT / vLLM backends; task definitions as Python modules or YAML |
| **Data flow** | Task registry → model adapter → batch generation → metric scorer (exact match, perplexity, multiple-choice accuracy) → aggregated JSON results. Supports few-shot prompt templates per task. |
| **vs. DataWatch** | Benchmark-centric, not production-centric. DataWatch's eval surface is closer to promptfoo's behavioral-test posture; lm-eval-harness adds the capability-tier concept (PAI-style ~70%/99% split) DataWatch lacks. No orchestration or session lifecycle. |

---

## 3. openai/evals

| Field | Value |
|---|---|
| **URL** | https://github.com/openai/evals |
| **License / Stars** | MIT · 19.4k★ |
| **Primary function** | Framework for evaluating OpenAI (and compatible) models; ships a registry of evaluation tasks and a runner that scores model responses against ground-truth or model-graded criteria |
| **Stack** | Python; OpenAI API; YAML/JSONL task specs; model-graded evals use a separate "grader" model call |
| **Data flow** | JSONL dataset → completion sampler → grader (exact match / model-graded / fuzzy) → per-sample result + aggregate report |
| **vs. DataWatch** | Narrower: OpenAI-API-only, no session lifecycle, no orchestration. The model-graded eval pattern (using a second LLM as grader) maps to DataWatch's verifier role in the PRD executor — both use an LLM to judge another LLM's output. |

---

## 4. DeepEval

| Field | Value |
|---|---|
| **URL** | https://github.com/confident-ai/deepeval |
| **License / Stars** | Apache-2.0 · 18.2k★ |
| **Primary function** | LLM evaluation framework focused on RAG and chatbot quality — answer relevancy, faithfulness, contextual precision/recall, hallucination detection; integrates with CI pipelines via pytest plugin |
| **Stack** | Python; OpenAI/Anthropic/local models for metric computation; pytest plugin for CI; Confident AI cloud dashboard optional |
| **Data flow** | Test dataset (input + expected output + retrieval context) → LLM-as-judge metric scorers → pytest assertions → pass/fail + metric dashboard. Continuous eval mode runs on schedule against live endpoints. |
| **vs. DataWatch** | Closest peer for "continuous eval" (regression alerts over time) and RAG-specific metrics DataWatch lacks (faithfulness, contextual recall). DataWatch's session lifecycle and agent orchestration are absent here; DeepEval is a quality-gate tool, not a runner. |

---

## 5. RAGAS

| Field | Value |
|---|---|
| **URL** | https://github.com/vibrantlabsai/ragas (moved from `explodinggradients/ragas`, 301) |
| **License / Stars** | Apache-2.0 · 15.7k★ |
| **Primary function** | RAG pipeline evaluation — measures answer faithfulness, answer relevancy, context precision/recall, and context entity recall without ground-truth answers (reference-free) |
| **Stack** | Python; LangChain / LlamaIndex integration; OpenAI/Anthropic/local judge models; Hugging Face datasets for benchmark corpora |
| **Data flow** | RAG output (question + answer + retrieved contexts) → decompose into claims → LLM-as-judge scoring per metric → composite RAGAS score. Integrations with LangChain callbacks capture RAG traces automatically. |
| **vs. DataWatch** | No overlap on session lifecycle or orchestration. RAGAS's reference-free scoring pattern (claim decomposition → judge) is the missing grounding-metric layer DataWatch's `docs_search` could adopt (synthesis proposal #3 / #4). |

---

## 6. Guardrails AI

| Field | Value |
|---|---|
| **URL** | https://github.com/guardrails-ai/guardrails |
| **License / Stars** | Apache-2.0 · 7.4k★ |
| **Primary function** | Input/output validation for LLM calls via declarative "rails" — structured output schemas, content validators (detect PII, toxicity, injection, etc.), and automatic re-ask loops on failure |
| **Stack** | Python; validators as plugins (OpenAI, Transformers, regex); RAIL spec (XML/YAML) or Pydantic models; hub-registered community validators |
| **Data flow** | RAIL spec → input guard → LLM call → output guard validators → on-fail action (re-ask, fix, exception) → validated output. Each validator is a typed function; the hub lets operators compose them. |
| **vs. DataWatch** | Guardrail library (SAST/secrets/deps), guardrail profiles, per-PRD/per-task override priority, and the BL369 injection guard cover the same vertical. DataWatch's multi-stage pipeline (warn → block) and LLM-adversarial classifier (synthesis proposal #5 / enhancement proposal 5) are the gap. Guardrails AI is application-layer; DataWatch adds daemon-level OS envelope context. |

---

## 7. DSPy

| Field | Value |
|---|---|
| **URL** | https://github.com/stanfordnlp/dspy |
| **License / Stars** | MIT · 37.8k★ |
| **Primary function** | Compositional LLM programming — define pipelines as typed signatures + modules, then compile (optimize) prompts and few-shot examples automatically against a metric, without manual prompt engineering |
| **Stack** | Python; supports OpenAI, Anthropic, Cohere, Ollama, vLLM; optimizers (BootstrapFewShot, MIPROv2, COPRO); JSON-serializable modules |
| **Data flow** | Typed signature (input fields → output fields) → pipeline modules (Predict, ChainOfThought, ReAct, RAG) → optimizer (tries prompt variants against metric on a trainset) → compiled program (frozen prompts + few-shots) |
| **vs. DataWatch** | Orthogonal to DataWatch's execution surface. DSPy's "compile" step is a prompt optimization harness; DataWatch has no equivalent. The skill-contributed CLAUDE.md rules / AGENT.md rules are the nearest analogue (guide the model without compiling). DataWatch orchestrates sessions; DSPy optimizes what happens inside one LLM call. |

---

## 8. LangGraph

| Field | Value |
|---|---|
| **URL** | https://github.com/langchain-ai/langgraph |
| **License / Stars** | MIT · 41.2k★ |
| **Primary function** | Stateful multi-agent graph execution engine — nodes are Python callables, edges are conditional transitions, state is a typed dict that persists across steps and can be checkpointed for resume/branch |
| **Stack** | Python; LangChain ecosystem; SQLite/Redis/Postgres checkpointers; LangSmith tracing optional; LangGraph Platform (managed) or self-host |
| **Data flow** | Graph definition → state initialization → node execution (LLM call, tool call, human-in-the-loop gate) → conditional edge routing → checkpoint write → next node. Interrupts can pause for human approval. |
| **vs. DataWatch** | Closest structural analog to DataWatch's PRD-DAG orchestrator (BL117) + autonomous PRD loop (BL24). Key differences: LangGraph lives in-process (Python library), while DataWatch's orchestrator is a daemon that manages OS-level tmux sessions; DataWatch adds per-session cost tracking, compute-node routing, and the verifier/quality-gate layer LangGraph lacks natively. LangGraph's checkpoint/branch/replay is DataWatch's most significant gap (session rollback BL29 is restart-only, not snapshot/replay). |

---

## 9. AutoGen (Microsoft)

| Field | Value |
|---|---|
| **URL** | https://github.com/microsoft/autogen |
| **License / Stars** | CC-BY-4.0 · 60.9k★ |
| **Primary function** | Multi-agent conversation framework — agents with configurable profiles (LLM, human-proxy, tool-enabled) communicate via message passing to solve tasks collaboratively; supports nested chat and critic/debate patterns |
| **Stack** | Python; OpenAI/Anthropic/local via litellm; Docker code execution sandbox; AutoGen Studio (no-code UI); v0.4 introduced an actor model (Magentic-One) |
| **Data flow** | Agent group → initiate chat with task → message round-trips between agent roles (AssistantAgent, UserProxyAgent, CriticAgent) → code execution via subprocess/Docker sandbox → termination on stop condition → final message |
| **vs. DataWatch** | AutoGen's multi-agent conversation maps to DataWatch's Council (BL260) for multi-persona debate and the parent/child session model for spawned workers. AutoGen has no equiv to DataWatch's session lifecycle (persistent tmux, cost/kill/rollback), routing registry, or PRD-DAG. DataWatch's tmux-backed persistent sessions are a stronger isolation story than AutoGen's subprocess sandbox. |

---

## 10. pydantic-ai

| Field | Value |
|---|---|
| **URL** | https://github.com/pydantic/pydantic-ai |
| **License / Stars** | MIT · 19.8k★ |
| **Primary function** | Type-safe LLM agent framework built on Pydantic — define agents with typed inputs/outputs, tools, and dependencies; strong test/mock support; designed for production reliability |
| **Stack** | Python (3.10+); OpenAI, Anthropic, Gemini, Groq, Ollama; Logfire tracing optional; async-first |
| **Data flow** | Agent definition (model + system prompt + tools + result type) → `agent.run(user_prompt)` → internal ReAct loop (tool call decisions) → Pydantic-validated output. Test mode replaces model with mock responses for unit testing. |
| **vs. DataWatch** | pydantic-ai is an in-process agent library; DataWatch is an agent orchestration daemon. Complement rather than competitor: a pydantic-ai agent would run inside a DataWatch session. The typed output and test/mock surface is a pattern DataWatch sessions lack — all session output is unstructured text until the verifier grabs a diff. |

---

## 11. LiteLLM

| Field | Value |
|---|---|
| **URL** | https://github.com/BerriAI/litellm |
| **License / Stars** | MIT · 58.2k★ |
| **Primary function** | Unified LLM API proxy — single OpenAI-compatible interface over 100+ providers; adds load balancing, fallback chains, rate-limit handling, cost tracking, caching, logging to observability backends |
| **Stack** | Python; proxy mode (FastAPI server) or library import; Redis for caching; Langfuse/Helicone/S3 logging callbacks; budget manager |
| **Data flow** | OpenAI-format request → provider router (round-robin, least-busy, fallback chain) → selected provider API → response with cost metadata → optional cache write + callback logging |
| **vs. DataWatch** | LiteLLM is the most direct analog to DataWatch's LLM registry + ComputeNode registry + ordered failover + BL6 cost tracking + BL20 routing rules + BL30 cooldowns. DataWatch adds eval-data-driven routing and per-session/per-PRD model override; LiteLLM adds a broader provider matrix (100+) and budget governance UI. |

---

## 12. Haystack (deepset)

| Field | Value |
|---|---|
| **URL** | https://github.com/deepset-ai/haystack |
| **License / Stars** | Apache-2.0 · 26.4k★ |
| **Primary function** | Component-based LLM pipeline framework for RAG, document Q&A, and agent workflows; pipelines are directed graphs of typed components (retrievers, generators, routers, rankers) |
| **Stack** | Python; document stores (Elasticsearch, OpenSearch, Qdrant, Weaviate, etc.); OpenAI/Anthropic/HF generators; YAML pipeline definitions; REST API serving via hayhooks |
| **Data flow** | YAML pipeline → component instantiation → run(input_data) → component graph traversal (each component emits typed outputs consumed by downstream inputs) → final outputs. Components are stateless; pipeline state is the data flowing between them. |
| **vs. DataWatch** | Haystack's pipeline graph is a computation graph (stateless components); DataWatch's PRD-DAG is a work orchestration graph (stateful sessions). Haystack's document-store integration maps to DataWatch's `docs_search` / memory surface but at a lower level (vector DB abstraction). No session lifecycle, cost tracking, or guardrail layer. |

---

## 13. Dify

| Field | Value |
|---|---|
| **URL** | https://github.com/langgenius/dify |
| **License / Stars** | SSPL / Apache-2.0 split · 154.8k★ |
| **Primary function** | LLM application development platform — visual workflow builder, RAG pipeline, agent app templates, model provider management, prompt engineering IDE, and API/embedding publishing |
| **Stack** | Python (backend) + Next.js (frontend); Postgres + Redis + Weaviate/Qdrant; Docker Compose self-host or Dify Cloud |
| **Data flow** | Visual workflow canvas → node graph (LLM call, retrieval, code, HTTP, conditional) → execute via workflow engine → output streamed via SSE. Datasets (indexed docs) feed retrieval nodes. Knowledge base is reusable across apps. |
| **vs. DataWatch** | Dify is a platform product comparable to the DataWatch PWA + PRD orchestrator combined, aimed at the "build LLM apps" use case vs. DataWatch's "run autonomous coding agents." Dify has the best skills/packaging analog (reusable knowledge bases, app templates). DataWatch has no visual workflow canvas; Dify has no daemon-managed session lifecycle or OS-level observability. |

---

## 14. Arize Phoenix

| Field | Value |
|---|---|
| **URL** | https://github.com/Arize-ai/phoenix |
| **License / Stars** | Elv2 · 11.4k★ |
| **Primary function** | AI observability platform — traces LLM calls and RAG pipeline steps (OpenInference spans), computes quality metrics in the UI, supports experiment tracking and prompt versioning |
| **Stack** | Python; OpenInference SDK (trace instrumentation); gRPC/HTTP span ingest; SQLite (local) or Postgres (cloud); React UI; Otel-compatible collector |
| **Data flow** | Instrumented app → OpenInference tracer → span export (gRPC/HTTP) → Phoenix server span store → UI queries (traces, sessions, experiments, aggregated metrics) |
| **vs. DataWatch** | The most direct analog to the data-provenance / lineage gap identified in synthesis.md. Phoenix captures what DataWatch doesn't export: per-LLM-call spans with token counts, latency, retrieval context, and hallucination scores in a standard OpenInference schema. DataWatch would be a Phoenix *producer* if it emitted OTel/OpenInference spans (synthesis proposal #2 / enhancement proposal 3). |

---

## 15. Langfuse

| Field | Value |
|---|---|
| **URL** | https://github.com/langfuse/langfuse |
| **License / Stars** | MIT (core) / ELv2 (enterprise) · 34.3k★ |
| **Primary function** | LLM observability and analytics — distributed tracing for LLM applications, prompt versioning, dataset/eval management, cost analytics, user-session grouping |
| **Stack** | TypeScript/Next.js + Python SDK; Postgres + Clickhouse; Docker Compose; Langfuse Cloud or self-host; integrates with LangChain, LlamaIndex, OpenAI SDK, LiteLLM |
| **Data flow** | SDK-instrumented app → trace/span/generation events ingested via REST → Clickhouse for analytics queries → Next.js UI for trace explorer, dashboards, prompt management. Evals can be attached to traces as scores. |
| **vs. DataWatch** | Closest peer to the "drift + AI feedback loops" theme (theme 2) — Langfuse is the reference implementation of trend dashboards over LLM call history. DataWatch has BL6 cost counters and session timelines but no Clickhouse-backed analytics queries or prompt versioning. DataWatch is the agent runner; Langfuse would sit upstream as an observability consumer if DataWatch emitted traces. |

---

## 16. OpenInference (Arize)

| Field | Value |
|---|---|
| **URL** | https://github.com/Arize-ai/openinference |
| **License / Stars** | Apache-2.0 · 1.2k★ |
| **Primary function** | Semantic conventions and instrumentation libraries for tracing LLM applications — defines a standard span schema for LLM calls, RAG retrievals, agent actions, and tool calls on top of OpenTelemetry |
| **Stack** | Python + JavaScript SDKs; OpenTelemetry spans with custom semantic attributes; exporters to any OTel collector (Phoenix, Jaeger, OTLP) |
| **Data flow** | Instrumentation decorators/patchers wrap LLM calls → produce OTel spans with LLM-specific attributes (model name, token counts, prompt/response text, retrieval docs, tool invocations) → OTLP export to collector |
| **vs. DataWatch** | OpenInference is not an application; it is a *standard* — the semantic convention DataWatch would adopt to emit spans. The span schema maps directly onto DataWatch's existing data: session parent/child → trace/span hierarchy, LLM call → `generation` span, memory retrieval → `retrieval` span, tool call → `tool` span. Adopting OpenInference is the "design spike" recommended in synthesis.md before building the full exporter. |

---

## Summary table

| # | Name | Category | Stars | License |
|---|------|----------|-------|---------|
| 1 | promptfoo | Eval / red-team | 24.9k | MIT |
| 2 | lm-evaluation-harness | Benchmark eval | 13.9k | MIT |
| 3 | openai/evals | Eval framework | 19.4k | MIT |
| 4 | DeepEval | RAG/chat eval | 18.2k | Apache-2.0 |
| 5 | RAGAS | RAG metrics | 15.7k | Apache-2.0 |
| 6 | Guardrails AI | Input/output validation | 7.4k | Apache-2.0 |
| 7 | DSPy | Prompt optimization | 37.8k | MIT |
| 8 | LangGraph | Agent orchestration | 41.2k | MIT |
| 9 | AutoGen | Multi-agent chat | 60.9k | CC-BY-4.0 |
| 10 | pydantic-ai | Type-safe agent library | 19.8k | MIT |
| 11 | LiteLLM | LLM proxy / routing | 58.2k | MIT |
| 12 | Haystack | RAG pipeline framework | 26.4k | Apache-2.0 |
| 13 | Dify | LLM app platform | 154.8k | SSPL/Apache |
| 14 | Phoenix | AI observability | 11.4k | ELv2 |
| 15 | Langfuse | LLM analytics / tracing | 34.3k | MIT/ELv2 |
| 16 | OpenInference | OTel span standard | 1.2k | Apache-2.0 |
