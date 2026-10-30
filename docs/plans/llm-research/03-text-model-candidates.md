# 03 — Text/Agentic Model Candidates (smaller/faster than qwen3.8:27b)

Compiled 2026-09-29 against the same live datawatch instance documented in
[01 — Current State](01-current-state.md) and [02 — Workload Profiles](02-workload-profiles.md).
Environment facts below were re-verified live this session via `llm_list`,
`compute_node_list`, and `ollama_stats` and match the brief exactly (no drift
since 01/02): registry entries `claude-code`, `ollama-datawatch`
(`auto_add_models: true`, 10 models incl. `qwen3.8:27b`, `gpt-oss:120b`,
`qwen3-coder-next:q4_K_M`), `ollama-local-node` (7-model list, stale vs.
the 4 actually installed locally per `ollama_stats`: `moondream:latest`,
`llmvision/glimpse-v1`, `qwen3:1.7b`, `nomic-embed-text`), `opencode` (node
`datawatch`), `openwebui-datawatch`; compute nodes `datawatch` (priority 90,
`max_concurrent_sessions: 1`, no declared hardware), `datawatch-openai`
(Blackwell B200 128 GB, `max_concurrent_models: 3`), `local-node` (32 GB GPU
32 GB, `max_concurrent_models: 10`, priority 70), `test-node` (duplicate
address of `datawatch`, priority 50, stale).

## A note on how this document was researched — read before trusting any single cell

Per scope, the only research tools available were the `web_search` MCP tool
(SearXNG proxy, default engine Bing) and the datawatch read-only MCP tools.
**`web_search` did not work for this task.** Roughly 20 queries were tried
across every candidate family in the brief, with increasingly specific and
then increasingly generic phrasing (exact model IDs in quotes, `site:`
filters, natural-language questions, review/comparison phrasing). The pattern
was consistent and reproducible: any query containing a specific model name
returned only that brand's generic homepage, its Wikipedia article, sign-in
pages, or unrelated pages sharing a keyword (`"Phi-4-mini-instruct microsoft
huggingface"` returned Greek-letter-Φ and HIPAA/PHI pages; `"gpt-oss-20b"`
returned chatgpt.com and a GPT-6 Astra press page; `"best small local LLM for
tool calling"` returned Best Buy and dictionary definitions of "best").
`site:ollama.com/library <model>` consistently returned only `ollama.com`'s
own homepage/download pages, never a resolved library tag page. This looks
like the search backend degrading to whichever single term has the most
generic global search volume rather than honoring the full query — a tool
limitation, not a refinement problem (retried per the web-search-guidance
skill's instruction, still failed).

**What *did* come through, and is cited below with real signal:**
- The Qwen3 family's own GitHub/Hugging Face/qwen.ai pages surfaced
  reliably (queries containing plain "Qwen3" without extra model-size
  qualifiers). Confirms the family exists, is actively maintained
  (`QwenLM/Qwen3` GitHub, "Qwen3-Instruct-2507" update mentioned in the repo
  description), and that a 235B-A22B MoE flagship and a 0.6B dense model both
  ship in the same family line. — github.com/QwenLM/Qwen3,
  huggingface.co/collections/Qwen/qwen3, qwen3.app (all checked 2026-09-29).
- `github.com/ollama/ollama`'s own repo description (checked 2026-09-29)
  currently reads *"Get up and running with Kimi, GLM, MiniMax, DeepSeek,
  gpt-oss, Qwen, Gemma and other models"* — live confirmation that gpt-oss,
  Qwen, Gemma, DeepSeek, GLM, and Kimi/MiniMax families are all present in
  Ollama's supported-model set as of today, even though the specific tag
  pages wouldn't resolve.
- A GLM search surfaced that the GLM line has moved well past "GLM-4.x" by
  now: z.ai's own subscribe page and openlm.ai (article dated 2026-08-26,
  checked 2026-09-29) reference **GLM-5.3, GLM-5.3-Flash, GLM-5.2, and
  GLM-4.7** as the current lineup. This means the brief's candidate
  "GLM-4.x small/air" is one to two major versions behind what Zhipu/Z.ai
  actually ships today — noted in the table below, but exact GLM-5.3-Flash
  parameter count/license could not be confirmed live.
- A Mistral search confirmed the company is still active and independent
  (Forbes, 2026-04-16, checked 2026-09-29: "$14 billion AI empire") and that
  its consumer product was renamed "Vibe" (formerly Le Chat) — no model-card
  specifics surfaced.

**Everything else in the per-model table below that isn't attributed to one
of the citations above is carried from training knowledge (cutoff January
2026) and is marked "training knowledge, unverified live" in the Evidence
column.** Given the GLM finding above — a real, confirmed step-change in just
the eight months since cutoff — treat every unverified row as a **floor**,
not a current spec: assume newer point releases exist for most families by
September 2026 and re-check with a working search tool (or `ollama pull
<model>:latest` / `ollama show`, which this project's scope did not permit)
before committing disk space or a routing config to any specific tag.

## MoE vs. dense for this fleet's goal

The stated goal (02) is **8+ calls running in parallel across CPU+GPU** on a
122 GB RAM / one-gpu-32gb host, plus a beefier but shared/priority-capped
`datawatch` node. Two shapes of "smaller than qwen3.8:27b" serve that
differently:

- **Sparse MoE with a small active-parameter count** (e.g., a 30B-total /
  3B-active design, the shape Qwen3's "A3B" tag denotes) does most of its
  matrix work over ~3B active parameters per token even though the full
  ~30B-parameter weight set must be resident in RAM/VRAM. That resident-vs-
  active split is exactly what suits a **CPU+GPU split on a high-RAM, single-
  midrange-GPU host**: keep the full weights in system RAM (122 GB comfortably
  fits several such models at once), let only the active-expert compute path
  matter for tokens/sec, and you get dense-7B-ish speed from a much smarter
  model. This is the best fit for `local-node`'s specific hardware profile
  and for the fast-small/balanced tiers that need many concurrent cheap
  calls (verifier, guardrail, Council personas, embedding).
- **Dense small models** (4B–14B) have no such split — every parameter is
  active every token — but their memory footprint is small and *fixed*,
  which makes them the right shape for **packing N independent full model
  instances into a fixed 32 GB VRAM budget** to hit real parallelism (e.g.,
  four Gemma-3-4B-class instances resident at once beats one MoE model that
  still serializes through a single loaded-model slot in Ollama). Dense
  small models are the better fit when the constraint is "many independent
  GPU-resident instances," not "one smart model split across RAM/VRAM."

Given `node:datawatch`'s confirmed `max_concurrent_sessions: 1` and
`local-node`'s `max_concurrent_models: 10`, the *local* 32 GB GPU host is
actually where dense-small packing has room to matter today; the MoE/CPU-
split trade is more about giving `datawatch` (currently one session at a
time, whatever the model) options that don't all pin the same 15.5 GB of
VRAM that `qwen3.8:27b` already holds.

## Candidate families

Sizes/specs marked "training knowledge, unverified live" per the note above.

| Family / variant | Total params (active, if MoE) | Disk size @ Q4_K_M / @ Q8 (approx.) | Context window | License | Ollama/llama.cpp tool-calling | Structured-output signal | Benchmark signal | Evidence |
|---|---|---|---|---|---|---|---|---|
| **Qwen3-4B-Instruct-2507** | 4B dense | ~2.5 GB / ~4.3 GB | 256K (native, via YaRN-free long-context variant) | Apache 2.0 | Yes, native Ollama tool-calling template | Qwen3 family ships an explicit non-thinking "Instruct" mode tuned for direct JSON/tool output (per repo description) | Qwen3 blog claims competitive IFEval/instruction-following for its size class | github.com/QwenLM/Qwen3 (checked 2026-09-29, confirms family + "Instruct-2507" mode exists); size/context: training knowledge, unverified live |
| **Qwen3-8B** | 8B dense | ~5.2 GB / ~8.9 GB | 128K | Apache 2.0 | Yes | Same family tool-calling template as above | Reported near Qwen2.5-14B on several benchmarks at half the size (training knowledge) | Already installed in this fleet's registry (`ollama-datawatch`, `ollama-local-node`) per live `llm_list` — existence/size on disk should be re-confirmed via `ollama show qwen3:8b` rather than assumed from the tag name alone (01 already flags qwen3:1.7b's tag as understating its real 2.0B param count) |
| **Qwen3-14B** | 14B dense | ~9 GB / ~15 GB | 128K | Apache 2.0 | Yes | Same | Closest dense sibling to the fleet's existing `qwen2.5:14b` | training knowledge, unverified live |
| **Qwen3-30B-A3B** (MoE) | 30.5B total / **3.3B active** | ~18–19 GB / ~32 GB | 128K–256K depending on release | Apache 2.0 | Yes | Strong — this shape is Alibaba's flagship "cheap-to-serve, smart" MoE design | Reported to approach much larger dense models on reasoning/coding while running at small-dense-model speed (training knowledge) | training knowledge, unverified live. **This is the textbook fit for the CPU+GPU-split argument above** — closest in spirit to what the brief is asking for |
| **Qwen3-Coder-30B-A3B** | 30B total / ~3B active | ~18–19 GB / ~32 GB | 256K+ | Apache 2.0 | Yes, tool-calling-first design (coding agent use case) | Purpose-built for agentic coding tool-call loops | Positioned as the small sibling to the fleet's existing `qwen3-coder-next` (79.7B) and `qwen3-coder:30b` (dense) — an MoE alternative at similar total size but far cheaper active compute | training knowledge, unverified live; fleet already runs the **dense** `qwen3-coder:30b` (18.6 GB, confirmed in 01) — this MoE sibling is a lateral option worth A/B-ing against it, not a strict upgrade |
| **Gemma 3 4B** | 4B dense | ~2.5 GB / ~4.3 GB | 128K | Gemma license (custom, commercial use allowed with use restrictions — not OSI-approved Apache/MIT) | Limited native tool-calling vs. Qwen3/Llama; workable via prompt-based function-calling | No strict-JSON guarantee built in; needs grammar-constrained decoding for hard JSON | Strong on instruction-following/knowledge for size, weaker on agentic tool-use benchmarks (training knowledge) | training knowledge, unverified live |
| **Gemma 3 12B** | 12B dense | ~8 GB / ~13 GB | 128K | Gemma license | Same caveat as above | Same caveat | Fleet already runs `Gemma3:12b` (8.1 GB, confirmed in 01, currently loaded) | Live: already in fleet (01, `ollama_stats` this session shows it's not currently loaded on `local-node`, only on `datawatch`) |
| **Gemma 3n** (mobile/edge variant) | ~4B effective footprint via per-layer embedding tricks, distinct from Gemma3-4B | Smaller than Gemma3-4B disk footprint | Reduced vs. full Gemma 3 | Gemma license | Limited | Designed for on-device/edge, not server tool-calling | Not evaluated as a serious candidate here — edge/mobile-optimized architecture, not built for the tool-calling/JSON workloads this fleet needs (training knowledge) | training knowledge, unverified live |
| **gpt-oss-20b** (OpenAI, MXFP4) | ~21B total, small-active MoE | ~13 GB (native MXFP4) | 128K | Apache 2.0 | Yes — OpenAI ships an explicit harmony/tool-calling chat format for this family | Explicit structured-output/function-calling support was a launch feature of gpt-oss | Positioned by OpenAI as near-o3-mini-class reasoning at a fraction of the size (training knowledge) | github.com/ollama/ollama repo description (checked 2026-09-29) confirms "gpt-oss" ships in Ollama's supported set; size/benchmark specifics: training knowledge, unverified live |
| **gpt-oss-120b** (incumbent, for contrast) | ~117B total, small-active MoE | 65.4 GB (confirmed live, 01) | 128K | Apache 2.0 | Yes | Yes | Already in fleet, not loaded (01) | Live: `llm_list`/01 |
| **Llama 4 Scout** (Meta) | MoE, 17B active / 109B total (per public reporting at launch) | Large even quantized — not a good fit for this host without further quantization | Very long (reported 10M-token class context at launch) | Llama 4 community license (commercial use allowed under Meta's terms, with restrictions above a very high MAU threshold that doesn't apply here) | Yes | Yes | Broad general capability at launch; active-param count still meaningfully larger than the Qwen3-A3B shape | training knowledge, unverified live — **do not bother** candidate, see below |
| **Phi-4** (dense, ~14B) | 14B dense | ~9 GB / ~15 GB | 16K (short relative to this fleet's needs) | MIT | Weaker native tool-calling than Qwen3/Llama families | Good at math/reasoning benchmarks, not a JSON/tool-calling specialist | Strong MMLU/GPQA-style reasoning for size (training knowledge) | training knowledge, unverified live |
| **Phi-4-mini** (~3.8B dense) | 3.8B dense | ~2.5 GB / ~4.3 GB | 128K | MIT | Explicit function-calling support added for this variant | Better structured-output story than base Phi-4 | Targeted at edge/agentic use cases per Microsoft's stated positioning | training knowledge, unverified live |
| **Phi-4-reasoning** | 14B dense, reasoning-tuned | ~9 GB / ~15 GB | 32K | MIT | Not a tool-calling specialist — reasoning-chain focus | N/A | Distilled/RL'd for chain-of-thought math/science reasoning | training knowledge, unverified live |
| **Mistral Small 3.x** (~22–24B dense) | ~22–24B dense | ~14–15 GB / ~24 GB | 128K | Apache 2.0 (recent Mistral Small releases) | Yes | Yes, Mistral publishes function-calling docs for this line | Competitive general + coding benchmarks near larger models (training knowledge) | training knowledge, unverified live |
| **Ministral 8B** | 8B dense | ~5.2 GB / ~8.9 GB | 128K | Mistral Research License (non-commercial) for some Ministral releases — **verify commercial terms per exact release before use** | Yes | Yes | Edge-focused Mistral line | training knowledge, unverified live — license caveat is the main risk here |
| **Devstral Small** (Mistral, coding-focused) | ~24B dense | ~15 GB / ~26 GB | 128K+ | Apache 2.0 | Yes, tool-calling-first for agentic coding | Purpose-built for SWE-bench-style agentic coding tasks | Positioned specifically against agentic coding benchmarks (training knowledge) | training knowledge, unverified live |
| **DeepSeek-R1-Distill-Qwen-14B / -32B** | 14B / 32B dense (distilled reasoning traces onto Qwen base) | 14B: ~9 GB/~15 GB; 32B: ~19 GB/~34 GB | Inherits Qwen base context (128K) | MIT (DeepSeek's distill releases) / Apache 2.0 base | Inherits Qwen tool-calling template | Reasoning-distilled, not tool-calling-optimized | Strong math/reasoning transfer from R1 at much lower cost (training knowledge) | training knowledge, unverified live |
| **GLM-4.5-Air / newer GLM-*-Flash** | MoE, small-active variant (Air/Flash tiers are Zhipu/Z.ai's cost-optimized tier) | Unconfirmed | Unconfirmed | Historically permissive (check current release — Zhipu has varied license terms across GLM generations) | Yes, GLM has a documented function-calling format | Yes | Positioned by Zhipu as a cost-efficient agentic tier | z.ai/openlm.ai (checked 2026-09-29) confirm the *line* has moved to GLM-5.3/5.2/4.7 by now — **the brief's "GLM-4.x small/air" target has likely already been superseded; find and evaluate whichever Flash/Air-tier model sits under the current GLM-5.x line instead of assuming GLM-4.5-Air is still current** |
| **NVIDIA Nemotron Nano** (~8–9B dense, distilled/pruned from Llama) | ~8B dense | ~5.2 GB / ~8.9 GB | 128K | NVIDIA Open Model License (commercial use generally allowed — verify per release) | Yes | Yes | NVIDIA publishes strong throughput-per-dollar claims specifically for their own hardware (TensorRT-LLM path); Ollama/llama.cpp path throughput is a different, unverified number | training knowledge, unverified live |
| **IBM Granite 4** (small MoE tier) | Small-active MoE, exact split unconfirmed | Unconfirmed | Unconfirmed, IBM has emphasized long-context in this line | Apache 2.0 | Yes, IBM markets Granite explicitly for enterprise tool-calling/agentic use | Yes — structured output is a stated design goal of Granite 4 | training knowledge, unverified live |
| **SmolLM3** (~3B dense, Hugging Face) | 3B dense | ~2 GB / ~3.3 GB | 64K–128K | Apache 2.0 | Limited — SmolLM's design goal is small-footprint general capability, not agentic tool-calling | Weak relative to purpose-built tool-calling models | Positioned as a fully open (data+weights+training-recipe) small model, not a benchmark leader | training knowledge, unverified live — **do not bother** for this fleet's workloads, see below |
| **Kimi / MiniMax families** | Various, several are very large MoE | Large | Large | Varies by release | Varies | Varies | Named in `ollama/ollama`'s own repo description as supported alongside gpt-oss/GLM/Qwen/Gemma/DeepSeek | github.com/ollama/ollama (checked 2026-09-29) — confirms these families are Ollama-servable at all; not otherwise evaluated here since neither was named in the brief's candidate list and no size-appropriate small variant could be confirmed |

## Incumbents, for comparison (what's actually being traded against)

| Model | Role today | Params (active) | Disk/VRAM | Confirmed live? |
|---|---|---|---|---|
| claude-code (opus/sonnet/haiku) | Interactive sessions, default backend | N/A — hosted API | N/A | Yes (`llm_list`) |
| qwen3.8:27b | PRD decomposition + coding workers (frontier/agentic tier) | 27.3B dense | 17.7 GB / 15.5 GB VRAM, currently loaded | Yes (01, re-confirmed this session) |
| gpt-oss:120b | Installed, not pinned to any call site | ~117B (small-active MoE) | 65.4 GB, not loaded | Yes (01, `llm_list` this session) |
| qwen3-coder-next:q4_K_M | Installed, not pinned to any call site | 79.7B dense | 51.7 GB, not loaded | Yes (01, `llm_list` this session) |
| qwen3-coder:30b | Installed, not pinned to any call site | 30.5B dense | 18.6 GB, not loaded | Yes (01) |

What the candidates above are trading against these incumbents: qwen3.8:27b
is a **27B dense** model occupying 15.5 GB of VRAM for both the highest-
stakes calls in the system (02, tiers "frontier/agentic"). Every MoE
candidate in the 30B-total/~3B-active shape (Qwen3-30B-A3B, Qwen3-Coder-30B-
A3B, gpt-oss-20b) offers a plausible path to **similar or better quality at
much lower active compute**, which is the actual lever for 02's "8+ parallel
calls" goal — but none of that has been benchmarked against this fleet's
real PRD-decomposition/coding-worker traffic, and 02 already flagged that
traffic's cost/duration as **unknown** across the board. Recommend measuring
qwen3.8:27b's real behavior on this workload (02, profile 1's stated highest-
value gap) before swapping the frontier tier to any unverified alternative.

## Recommendations by workload tier (from 02)

### Fast-small tier (verifier, guardrail, memory embedding, fact extraction/summarization)
1. **Qwen3-4B-Instruct-2507** — small, Apache-2.0, native tool-calling
   template, and the verifier/guardrail workloads (02, profiles 3-4) are
   bounded-input/small-output exactly where a 4B dense model is cheapest to
   run many-at-once.
2. **Phi-4-mini** — MIT license removes any license-review friction, and its
   explicit function-calling tuning fits the guardrail call site's need for
   a clean pass/warn/block verdict.
3. **gpt-oss-20b** — if the fast-small tier ever needs one model to also
   cover occasional short reasoning (e.g., a guardrail rule needing a chain
   of checks), its MXFP4 quantization keeps it cheap enough to still fit this
   tier while punching above a typical 4B model.

### Balanced tier (Council personas, vision, voice/STT once wired up)
1. **Qwen3-30B-A3B (MoE)** — 02 flags Council personas as the "most
   naturally parallel workload" with `council.max_parallel` currently capped
   below its 12-wide natural shape; an MoE model with ~3B active parameters
   is the correct shape to raise that parallelism without linearly raising
   compute cost per additional concurrent persona.
2. **Gemma 3 12B** — already installed and loaded on this fleet (01); a
   known-good, already-paid-for option for persona proposals that don't need
   tool-calling (02 confirms personas have no tool-calling requirement).
3. **Mistral Small 3.x** — Apache-2.0, strong general critique/reasoning
   quality, a reasonable fallback for `council.llm_ref` if Qwen3-30B-A3B's
   MoE serving path proves awkward under Ollama's single-model-slot-per-node
   behavior.

### Strong tier (Council synthesis, interactive chat sessions)
1. **Keep interactive chat sessions on claude-code** — 02 explicitly says
   this tier is correctly pinned today and is the one workload where per-
   token latency directly affects a waiting human; none of the candidates
   here beat a hosted frontier model on that dimension.
2. **Qwen3-14B or Qwen3.5:35b (already installed)** for Council synthesis —
   02 warns synthesis "should not be downsized as aggressively as persona
   calls" since it must fairly reconcile 12 viewpoints; the already-installed
   `qwen3.5:35b` (23.9 GB, confirmed in 01) is a safer default than
   introducing a new small model here, with Qwen3-14B as a lighter fallback
   only if synthesis latency becomes a bottleneck.

### Frontier/agentic tier (PRD decomposition, coding worker sessions)
1. **Do not swap qwen3.8:27b yet** — 02's own summary table calls this tier
   "already correctly pinned," and the cost/duration data needed to justify
   any change is marked unknown across the board (02, profiles 1-2). Measure
   first.
2. **Qwen3-30B-A3B (MoE) as a benchmarked challenger** — same total
   footprint class as `qwen3-coder:30b` already in the registry, but with
   the active-parameter shape this document's MoE argument favors; worth an
   A/B decomposition run against qwen3.8:27b's real PRD output before any
   routing change, not a blind swap.
3. **gpt-oss-20b as a lightweight fallback**, not a primary — its strict
   tool-calling/harmony format is a good match for the injection-guard-
   hardened decomposeFn call site (02 confirms this call site has extra
   `<user_data>` wrapping), but at ~1/5th the disk size of qwen3.8:27b, it is
   a downgrade path for load-shedding, not a quality upgrade.

## Do not bother

- **Llama 4 Scout** — its public active-parameter count (~17B active) is
  larger than every MoE candidate recommended above while offering no clear
  quality edge for this fleet's specific workloads (structured JSON,
  critique, tool calling); its long-context headline feature isn't a
  bottleneck for any profile in 02. Also carries Meta's community license
  terms, which need separate legal review this project's scope doesn't cover.
- **Gemma 3n** — purpose-built for mobile/edge inference, not server-side
  tool-calling or structured JSON; the architectural tricks that make it
  small on a phone don't translate into an advantage on a 122 GB RAM / RTX
  32 GB GPU host that already has room for dense Gemma 3 4B/12B.
- **SmolLM3** — fully open in spirit (a real virtue, and worth citing for a
  transparency-sensitive use case) but not built or benchmarked for agentic
  tool-calling, which disqualifies it from the worker/decomposition tiers
  and gives it no edge over Qwen3-4B/Phi-4-mini in the fast-small tier.
- **Ministral 8B** — the license terms on Mistral's "Research" branded
  releases have historically restricted commercial use; unless the exact
  release in question is confirmed Apache/commercial-clear, this is a legal-
  review blocker, not a technical one, and Qwen3-8B/Phi-4-mini cover the same
  size class without that risk.
- **Kimi / MiniMax large variants** — confirmed present in Ollama's
  supported-model set (see citation above) but every publicly known variant
  in these families is large-MoE at a scale closer to this fleet's existing
  `gpt-oss:120b`/`qwen3-coder-next` than to anything "smaller/faster than
  qwen3.8:27b" — out of scope for this document's brief.
- **DeepSeek-R1-Distill-Llama-70B and other 70B-class distills** — too large
  to be a "smaller/faster" candidate against a 27B incumbent; the 14B/32B
  distills already listed above are the right size band from this family.

## Gaps and next steps

- **Re-run this survey with a working search tool or direct `ollama pull` /
  `ollama show` access.** This session's `web_search` tool could not resolve
  a single specific model-card or Ollama-library-tag page despite ~20 varied
  queries; every non-training-knowledge fact above had to come from whatever
  generic top-level page a brand name's homepage/Wikipedia article/GitHub
  repo description happened to surface. That is a real gap in this
  document's confidence, not a stylistic hedge — flag it to whoever reviews
  this before using it to justify a routing change.
- **GLM finding is the strongest evidence the "training knowledge" rows are
  stale**: the GLM line has already moved two major versions past what the
  brief listed as a candidate, confirmed live in the same eight months since
  this assistant's January 2026 knowledge cutoff. Treat every other family's
  "current version" as similarly suspect until re-checked.
- **The actual blocking question from 02 is still unanswered**: no cost or
  duration baseline exists for qwen3.8:27b on real PRD-decomposition/coding-
  worker traffic (02, profiles 1-2, "highest-value gap"). Every
  recommendation above is a *quality-shape* argument (MoE active-params,
  dense-packing, license, tool-calling support), not a measured
  performance comparison — measuring the incumbent should happen before
  benchmarking any challenger against it.

## Verification log

- **Re-verified 2026-09-30 (retry pass, fresh session):** re-ran `llm_list`,
  `compute_node_list`, and `ollama_stats` live. Zero drift from every fact
  in this document's opening paragraph and the incumbents table: same 5
  registry entries (`claude-code`, `ollama-datawatch` with `auto_add_models`
  and the same 10-model list including `qwen3.8:27b` and `gpt-oss:120b`,
  `ollama-local-node` still carrying the same stale 7-model list,
  `opencode` on node `datawatch`, `openwebui-datawatch`); same 4 compute
  nodes with identical priorities (`datawatch` 90, `datawatch-openai` 90,
  `local-node` 70, `test-node` 50, stale duplicate address unchanged);
  local `local-node` Ollama still exactly 4 models (`moondream:latest`,
  `llmvision/glimpse-v1`, `qwen3:1.7b`, `nomic-embed-text`, 7.8 GB total).
  This document's prior commit (`0309b41`) needed no factual correction —
  this pass exists to close out a retry whose failure was an infrastructure
  error (session-spawn/verifier context-cancellation), not a content issue.
  The candidate-model research gap flagged above (`web_search` unable to
  resolve model-card/tag pages) was not re-attempted this pass since no new
  search capability is available in this retry; it remains the document's
  primary open gap.
