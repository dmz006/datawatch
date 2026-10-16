# llm-research

Research and documentation project auditing how the `datawatch` orchestration
system actually uses LLMs today: which compute nodes and models back each
consumer (PRD planning, worker sessions, verification, guardrails, Council
Mode, vision, voice, memory), where drift has crept in between the LLM/compute
registries and live reality, and where cost/eval observability currently falls
short. Findings are re-verified against the live system via read-only MCP
tools and Ollama API calls rather than taken on faith from prior notes; each
doc states its verification date.

## How to read this

Start with **[08 — Recommendations](docs/08-recommendations.md)** — it's the
executive document, self-contained, and states its own findings, phased
rollout, risks, and the decisions still left to the operator. Read 01-03 and
07 only if you want the underlying evidence for a specific claim in 08 — 08
links to the exact section of each every time it cites one. 01 is the
foundational fact-base (hardware, registry, drift, call sites); 02 groups
those call sites into workload tiers; 03 surveys model candidates per tier;
07 proposes concrete datawatch feature changes (routing/tiering, then
usage/cost/performance tracking) to act on 01-03's findings.

**A numbering note, stated plainly rather than hidden:** this project's
original planned index (04 Cost & Observability, 05 Council & Orchestration
Deep Dive, 06 Vision & Voice Pipelines, 07 Capacity & Scheduling Under Load,
08 Security & Guardrails, 09 Recommendations) drifted during execution.
What actually got written under 07 and 08 covers different, overlapping
ground under different numbers — 07 became a combined
routing/tiering/observability-improvements doc, and 08 became the
recommendations doc originally planned as 09. **04 was subsequently
commissioned and written under the original 06 topic (Vision & Voice, plus
embeddings) rather than its own planned Cost & Observability topic** — see
the note at the top of 04 itself. **05 was later commissioned as "Serving
and Hardware"** (concurrency internals, engine alternatives, CPU inference,
VRAM worksheets, multi-node balancing) rather than its originally planned
"Council & Orchestration Deep Dive" topic. **06 was later commissioned as
"Benchmark Plan"** (how to validate 03/04's candidates with Evals-suite and
throughput evidence before any routing change) rather than its originally
planned "Vision & Voice Pipelines" topic — that content lives in 04 instead,
per the note above. 08 explains the pre-04 gap in its own scope note; 04, 05,
and 06 each explain their own filename/topic mismatch inline. Treat the list
below as what's actually on disk, not the original plan.

## Index

- [01 — Current State](docs/01-current-state.md) — hardware, model inventory, LLM registry + drift, call-site map, capacity/scheduling, observability gaps. **Done.**
- [02 — Workload Profiles](docs/02-workload-profiles.md) — per-consumer token/latency/quality/parallelism profile for every LLM call site, routed into fast-small / balanced / strong / frontier-agentic tiers. **Done.**
- [03 — Text Model Candidates](docs/03-text-model-candidates.md) — survey of open-weight text/agentic models smaller/faster than qwen3.8:27b, MoE-vs-dense trade-off, per-tier recommendations. **Done.**
- [04 — Multimodal Candidates](docs/04-multimodal-candidates.md) — vision, voice/speech, and embedding model survey (covers the originally-planned 06 topic under the 04 filename — see note above); includes the live finding that a Whisper-based voice-transcription pipeline already exists but no TTS does. **Done.**
- [05 — Serving and Hardware](docs/05-serving-and-hardware.md) — Ollama concurrency internals, engine alternatives (vLLM/SGLang/llama.cpp), CPU inference on the Threadripper, CPU+GPU MoE offload, VRAM budget worksheets, multi-node balancing (numbered/scoped differently than originally planned — see note above). **Done.**
- [06 — Benchmark Plan](docs/06-benchmark-plan.md) — proposed Evals suites mirroring real workloads, performance metrics and how to read them from Ollama/nvidia-smi, a results template and quality/throughput decision rule, and a cheapest-first run order; references the in-progress, unshipped "eval sweep" feature found in memory (numbered/scoped differently than originally planned — see note above). **Done.**
- [07 — Datawatch Improvements](docs/07-datawatch-improvements.md) — Part A: routing/tiering/compute-balancing proposals; Part B: usage, cost, and performance tracking proposals. **Done** (numbered/scoped differently than originally planned — see note above).
- [08 — Recommendations](docs/08-recommendations.md) — the executive document: findings, model-per-tier table, target parallel-call layout, illustrative config changes, phased rollout, risks, and operator decisions. **Done.**
