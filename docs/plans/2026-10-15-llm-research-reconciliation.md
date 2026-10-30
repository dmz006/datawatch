# LLM research reconciliation (BL460)

Source: [llm-research/](llm-research/) (copied into this repo 2026-10-15
from the original research project; PRD `a2833a5e`, completed 2026-09-30,
decomposed/executed on Claude). Full detail lives there; this doc maps its
recommendations onto what's actually happened since, so the gap is clear
without re-reading all 8 source documents.

## Already validated independently

- **"Keep qwen3.8:27b for PRD planning / coding-worker sessions"**
  ([llm-research/08-recommendations.md](llm-research/08-recommendations.md) §2) —
  confirmed again 2026-10-15 via direct live testing (BL441, BL427 Phase 2,
  BL431 Phase 1 all running real tasks on it). Independent re-confirmation,
  not a restatement — the 09-29 research and the 10-15 live testing never
  referenced each other.
- **Ollama version skew flagged as a real risk** ("0.33.2 on `datawatch` vs
  0.24.0 on `local-node`... could silently affect quantization-format
  support if models move between hosts") — this exact problem caused
  Swift's capability-detection failure and was fixed 2026-10-15 (local
  upgraded to 0.40.5). The research predicted this two weeks before it was
  hit live.

## Not yet addressed — real, scoped, pending work (this is BL460)

1. **Verifier/guardrail model swap.** The verifier has drifted onto
   `qwen3.8:27b`@`datawatch`, contending with the planner/worker for that
   node's single session slot. Recommendation: a smaller fast model
   (Qwen3-4B-Instruct-2507 or Phi-4-mini) on `local-node` instead — frees
   the contention, and a verifier doesn't need frontier-tier capability.
2. **Council persona model upgrade.** Personas currently fall back to
   `qwen3:1.7b` (the fleet's smallest model) including adversarial personas
   (`hacker`/`app-hacker`/`security-skeptic`) that would benefit from more
   capability. Recommendation: Gemma3:12b — already installed and loaded on
   `local-node`, the research's own words were "zero-effort quick win."
3. **CPU-node isolation for voice/embedding.** Whisper STT and
   `nomic-embed-text` currently share GPU residency with Ollama traffic on
   `local-node`; moving them to a CPU-pinned path frees that GPU headroom
   for the balanced-tier models above. Note: a CPU-only Ollama instance
   existed for exactly this reason and was removed 2026-10-15 as redundant
   once the main instance got its config restored — if this is picked up,
   confirm whether a dedicated CPU path is still needed or whether
   per-request `num_gpu: 0` on the single instance covers it.
4. **Cost/eval observability gap.** `cost_summary` showed 2,302 sessions at
   `$0`/`0 tokens` across all backends as of the research; `eval_list_suites`
   was `[]`. No routing decision in the research could be benchmarked
   against real cost/quality data because of this. Cross-reference BL414
   (PRD cost rollup) and BL405 Phase 4 (per-call usage record store) —
   this gap may already be in scope there; confirm before treating it as
   net-new.
5. **`max_concurrent_sessions`/`council.max_parallel` config ambiguity.**
   The research found two authoritative docs disagreeing on whether
   Council synthesis runs parallel-2 or serial by default. Worth a quick
   direct code check before BL460 implementation starts.

## Explicitly not re-litigated

Everything else in the research's per-workload table (vision, voice/STT,
memory embedding, fact extraction, interactive sessions) was already
"keep as-is" — no action needed, not a gap.
