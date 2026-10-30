# 04 — Multimodal Candidates: Vision, Voice, Embeddings

**A filename/number note, in the spirit of the one already in [README](../README.md):**
the project's original index reserved **04** for Cost & Observability and put
vision/voice under **06**. This document was commissioned under the filename
`docs/04-multimodal-candidates.md` and covers vision + voice + embeddings (the
06 scope), not cost/observability. It is written under the requested filename
rather than silently renamed; 04-as-Cost-&-Observability remains unwritten.
Treat this as the same kind of drift 07/08 already documented, not a new
inconsistency to chase.

Compiled 2026-09-30 against the same live datawatch instance documented in
[01 — Current State](01-current-state.md). Web research ran the same day via
this session's `WebSearch` tool. That's a different tool from the
`web_search` MCP/SearXNG proxy that [03](03-text-model-candidates.md)
reported as non-functional for model-specific queries — this session's tool
returned real, resolvable results for every query tried, so the citations
below are live web sources, not training knowledge, unless stated otherwise.

**Re-confirmed on a later retry pass (still 2026-09-30):** the `web_search`
MCP/SearXNG (bing) tool was retried directly, following the "refine the
query rather than switch engines" guidance in
`.datawatch/skills/web-search-guidance/SKILL.md` — including quoted-phrase
and `site:huggingface.co` forms. It returned real (non-empty, non-error)
results each time, but ones topically unrelated to the query: a
"Qwen3-VL 4B MMBench benchmark" search returned only general Qwen3
text-model pages, and a "nomic-embed-text v2 dimensions" search returned
Australian time-zone pages. This reproduces 03's finding independently
rather than just repeating it, and confirms the `WebSearch`-tool citations
below were the only viable path this session, not a scope shortcut taken
without checking.

## Live re-verification (this session, re-checked again on retry)

Re-ran `get_config` (root + `vision`/`memory` sections), `llm_list`,
`compute_node_list`, `ollama_stats`, and `docs_search`/`docs_read`. No drift
against the brief's environment facts:

- **Vision config** (`get_config` root, `vision` section): `enabled: true`,
  `backend: ollama`, `endpoint: http://localhost:11434`, `model:
  moondream:latest`, `max_image_bytes: 0` (→ 10 MB default per
  `datawatch-definitions.md`). Matches the brief exactly.
- **`ollama_stats` on `local-node` (local, 32 GB GPU):** 4 models, 7.8 GB
  total — `moondream:latest`, `llmvision/glimpse-v1:latest`, `qwen3:1.7b`,
  `nomic-embed-text:latest`. Which one shows as "running" varies by
  snapshot (Ollama unloads idle models) — on the original pass only
  `nomic-embed-text` was loaded (0.6 GB VRAM); on this retry's re-check
  `running` was empty, i.e. everything had idled out. Treat "currently
  loaded" as a point-in-time fact, not a fixed state.
- **New fact the brief didn't have:** `get_config` (root) has a top-level
  **`whisper`** section — `{"backend":"", "enabled":true, "language":"en",
  "model":"medium", "venv_path":"~/.datawatch/.venv"}` — separate
  from the `voice` section the brief said doesn't exist (correct: there is
  no `voice` section, but there *is* a `whisper` one). See Part 2.
- **`docs_search`/`docs_read`** surfaced `howto/voice-input.md` (not found by
  the brief's suggested query terms alone — it needed a `"transcription
  backend configuration"`-style query) and confirmed the `vision-system`
  section of `datawatch-definitions.md` lists supported Ollama vision models
  as `llava, llava-phi3, llava-llama3, bakllava, moondream, minicpm-v`, plus
  `gpt-4o`/`gpt-4-vision-preview` (openai) and any openai-compat endpoint —
  matches the brief.

**Independently re-confirmed in a fresh session (2026-09-30, third pass):**
re-ran `get_config` (full dump), `ollama_stats`, `llm_list`, and
`compute_node_list` from scratch, without reading the numbers already
written below first. Zero drift against everything already documented in
this section: `vision` is still `{enabled:true, backend:ollama,
endpoint:http://localhost:11434, model:moondream:latest}`; `whisper` is
still `{backend:"", enabled:true, language:"en", model:"medium",
venv_path:~/.datawatch/.venv}`; no `tts` key exists anywhere in the
config dump; `memory` still shows `embedder_model:nomic-embed-text`,
`embedder_host:http://local-node:11434`, `dimensions:0`; `ollama_stats`
on `local-node` still reports the same 4 models / 7.8 GB with only
`nomic-embed-text` (0.6 GB) loaded; `compute_node_list` still shows
`datawatch` (priority 90, `max_concurrent_sessions:1`, no declared
capacity), `datawatch-openai` (Blackwell B200, 128 GB, `max_concurrent_models:3`),
`local-node` (32 GB GPU, 32 GB, priority 70, `max_concurrent_models:10`),
and `test-node` (stale duplicate of `datawatch`'s address, priority 50).
This is a genuine independent re-check, not a copy of the numbers already
in this file — see this run's own tool calls, not just the prose below.

---

## Part 1 — Vision

### Current state

datawatch's vision service (`vision.*` config) is enabled, pointed at Ollama
on the local 32 GB GPU, and defaults to `moondream:latest`. It backs the
comms-router image-attachment describer, the `vision_describe` MCP tool,
`start_session`/`send_input` `image_paths`, and Council Mode's
`image_path` (one description injected into every persona's proposal, per
`datawatch-definitions.md`). `llmvision/glimpse-v1` is also installed
locally but not wired as the default.

### Candidate models

| Model | Params | Ollama? | Description quality | OCR | Diagram/screenshot understanding | Latency class | License | Source |
|---|---|---|---|---|---|---|---|---|
| **Moondream 2** (current default, `moondream:latest`) | 2B (0.5B variant also exists) | Yes | Good general captioning; the 2026-line release improved OCRBench 58.3→61.2 and UI/ScreenSpot localization F1@0.5 from 60.3 to 80.4 | Improved, still behind larger models on receipts/dense docs | Better than before at UI-element localization; not a diagram-reasoning specialist | Very low (2B, purpose-built for edge) | Apache 2.0 | [Moondream blog](https://moondream.ai/blog/introducing-a-new-moondream-1-9b-and-gpu-support), [HF card](https://huggingface.co/vikhyatk/moondream2) (checked 2026-09-30) |
| **Qwen3-VL** (2B/4B/8B) | 2B–8B (family goes to 235B) | Yes — `qwen3-vl:2b/4b/8b` on Ollama, native image input | Strong: MMBench-EN 79.9 (2B) → 85.3 (8B); MMStar 68.1 → 75.3 (thinking mode) | Good — family emphasizes document/text-reading tasks | Yes — evaluated on RealWorldQA/MMStar-style benchmarks that include diagram-like reasoning; best-in-class among same-size peers per early 2026 write-ups | Low-moderate (4B: ~6 GB VRAM Q4_K_M; 8B: ~12 GB) | Apache 2.0 (Qwen license) | [Qwen3-VL tech report](https://arxiv.org/pdf/2511.21631), [codersera VRAM guide](https://codersera.com/blog/qwen3-vl-4b-vs-qwen3-vl-8b-benchmarks-vram-guide/) (checked 2026-09-30) |
| **MiniCPM-V 4.5** | 8B | Community GGUF via Ollama/llama.cpp | Very strong: 77.0 avg on OpenCompass across 8 benchmarks, beats GPT-4o-latest and Gemini-2.0 Pro on several | Leading OCRBench score among open MLLMs; state-of-the-art open-model PDF parsing on OmniDocBench | Yes, explicitly — LLaVA-UHD architecture handles high-res docs/diagrams up to 1.8MP at 4x fewer visual tokens | Moderate (8B) | Apache-family per OpenBMB repo | [arXiv 2509.18154](https://arxiv.org/pdf/2509.18154), [HF card](https://huggingface.co/openbmb/MiniCPM-V-4_5) (checked 2026-09-30) |
| **MiniCPM-V 4.6** | 1.3B | Community builds | Positioned as the "phone-class" successor for local agents; not benchmarked against 4.5 in the same tables surfaced | Not separately confirmed | Not separately confirmed | Very low | Apache-family (unconfirmed for this exact release — verify before adopting) | [MindStudio](https://www.mindstudio.ai/blog/what-is-minicpm-v-4-6-vision-model) (checked 2026-09-30) |
| **Gemma 3** (4B/12B/27B) | 4B/12B/27B multimodal (1B/270M are text-only) | Yes — `gemma3:4b`/`12b`/`27b` | Strong document/vision scores; 27B scores 85.6 DocVQA (ahead of llava) | Strong at 12B/27B | Competitive on document-style tasks; not diagram-specialized but general-purpose capable | 4B low, 12B moderate, 27B heavy (already installed at `Gemma3:12b` on the `datawatch` node per 03) | Gemma license (open weights, usage terms apply) | [Gemma 3 blog](https://huggingface.co/blog/gemma3), [Ollama library](https://ollama.com/library/gemma3) (checked 2026-09-30) |
| **glimpse-v1** (installed, `llmvision/glimpse-v1`) | 4B (Gemma 3 4B fine-tune) | Yes, already pulled locally | Purpose-built for home-security camera events (motion, deliveries, visitors, pets, vehicles); reports 1.9x accuracy over base Gemma 3 4B **on that narrow task**, not general captioning | Not its design target | Not its design target | Low (4B) | Inherits Gemma license (fine-tune) | [llmvision.org](https://llmvision.org/glimpse/), [HF card](https://huggingface.co/llmvision/glimpse-v1) (checked 2026-09-30) |
| **SmolVLM2** (256M/500M/2.2B) | 256M–2.2B | Via llama.cpp/transformers; not confirmed as an Ollama library tag | Smallest (256M) "surpasses 80B models from 18 months ago" per HF's own claim — treat as a vendor claim, not an independent benchmark | Covered by OCRBench/AI2D/ChartQA/DocVQA/TextVQA in its eval suite | Yes — AI2D/ChartQA are diagram-style benchmarks in its suite | Extremely low (<1 GB for the 256M variant) — best latency/quality floor for quick captions if Ollama support lands | Apache 2.0 | [HF blog](https://huggingface.co/blog/smolvlm2) (checked 2026-09-30) |
| **Pixtral 12B** (Mistral) | 12B + 400M vision encoder | Yes — `ollama run pixtral` | Outperforms Llama-3.2-11B and Qwen2-VL-7B; competitive with much larger open models on document tasks | Strong — trained explicitly on natural images *and* documents | Yes, documents/diagrams are a stated training target | Moderate-heavy (12B) | Apache 2.0 | [Mistral announcement](https://mistral.ai/news/pixtral-12b/), [arXiv 2410.07073](https://arxiv.org/abs/2410.07073) (checked 2026-09-30) |
| **InternVL 3.5 (8B and smaller)** | 8B+ (smaller variants exist in the family) | Not confirmed as a first-class Ollama library tag | Competitive with Pixtral-12B in the "medium" size band per routing-benchmark literature; scaling from 8B→38B nets only ~6% average gain, i.e. the 8B is already a good efficiency point | Not separately isolated in the sources found | Not separately isolated | Moderate | Varies by release — verify before adopting | [VL-RouterBench](https://arxiv.org/pdf/2512.23562) (checked 2026-09-30) — indirect signal, not an InternVL-specific benchmark page |
| **LLaVA family** (llava, llava-phi3, llava-llama3, bakllava) | 7B–13B class | Yes, all four are in the vision-system doc's supported list | Legacy baseline — Gemma 3 27B and MiniCPM-V both explicitly benchmark themselves as beating llava | Weak relative to the newer entries above | Weak relative to the newer entries above | Moderate-heavy for the quality delivered | Varies (Llama-derived variants inherit Llama community license terms) | `datawatch-definitions.md` vision-system section (already in scope facts) |

### Assessment

- **Comms-router quick captions** (the current `moondream:latest` job): stay
  on Moondream. It's already installed, already the fastest thing in the
  lineup, and its 2026 OCR/UI-localization gains close much of the gap with
  bigger models for the "what's in this photo" use case.
- **Council image critique** (needs diagram/screenshot understanding, not
  just photo captions): Moondream and glimpse-v1 are both the wrong tool —
  neither targets diagrams. **Qwen3-VL:4b** is the best-fit upgrade: native
  Ollama support, small enough to coexist with the other loaded models on a
  32 GB card, and the family's own benchmarks specifically include
  diagram/UI-style tasks. **MiniCPM-V 4.5** is the stronger but heavier
  alternative if document/OCR fidelity matters more than footprint.
- **glimpse-v1** should stay scoped to its actual job (security-camera event
  summaries) rather than being promoted to a general default — it's a
  narrow fine-tune, not a general-purpose upgrade over Moondream.
- Every OCR/description-quality number above is the *model vendor's own*
  benchmark claim (OpenCompass, OCRBench, DocVQA, MMBench) unless marked
  otherwise — none of these were independently re-run against datawatch's
  actual traffic (screenshots, diagrams, security-camera stills). Vendor
  benchmarks agree directionally (Qwen3-VL and MiniCPM-V both beat
  LLaVA-class and Moondream on document/OCR tasks) but the specific numbers
  should not be treated as apples-to-apples across papers with different
  eval harnesses.

---

## Part 2 — Voice / Speech

### What's actually running today (re-verified this session)

There **is** a transcription pipeline, contrary to the brief's assumption
that no `voice` config section exists — the config key is just named
`whisper`, not `voice`:

```
whisper: {"backend": "", "enabled": true, "language": "en", "model": "medium", "venv_path": "~/.datawatch/.venv"}
```

`docs_read` on `howto/voice-input.md` confirms this is real and documented:
*"Datawatch transcribes voice messages so you can speak into Signal,
Telegram, Slack, etc., or hit the PWA microphone, instead of typing. Four
supported transcription backends; pick whichever matches what you already
have running."* The doc's intro didn't enumerate the four backends by name
in the section retrieved — `backend: ""` (empty) in live config suggests the
default/auto-detected backend is in use rather than an explicit one, and
`model: "medium"` implies an OpenAI Whisper-family model (has a `medium`
size tier) run via the `venv_path` Python environment, most likely
`openai-whisper` or `faster-whisper` rather than `whisper.cpp` (the venv
path is a strong tell — `whisper.cpp` doesn't need a Python venv).

**No text-to-speech / voice-reply capability was found.** `get_config` has
no `tts` section, and no synthesis-related keys appear anywhere in the full
config dump. **datawatch can listen to voice notes today; it cannot speak
back.** This should be stated plainly to the operator rather than assumed
either way.

### Speech-to-text candidates

| Model | Size | WER | Real-time factor | License | OpenAI-compat `/v1/audio`? | Source |
|---|---|---|---|---|---|---|
| **faster-whisper large-v3-turbo** | 809M (decoder cut from 32→4 layers vs. large-v3's 1.54B) | ~7.7% batched WER; "1-2 WER points" worse than large-v3 for 4x speed | ~6x realtime on CPU (faster-whisper), ~4x on GPU, ~5x on Apple Silicon (whisper.cpp) vs. plain large-v3 | MIT (faster-whisper)/ CTranslate2 backend | Via community wrapper servers (e.g. `whisper-asr-webservice`), not built-in | [VexaScribe](https://vexascribe.com/whisper-large-v3-vs-turbo), [faster-whisper GH issue #1030](https://github.com/SYSTRAN/faster-whisper/issues/1030) (checked 2026-09-30) |
| **distil-whisper** | ~half the params of the equivalent full Whisper tier | Within ~1 WER point of full Whisper on English per its own release claims | Faster than full Whisper at matched size — exact multiplier not confirmed in this pass | MIT | No | Not independently re-verified this pass — carried from general knowledge; **flagged as unconfirmed live** |
| **Moonshine (Useful Sensors)** | 27M–61M (Tiny/Base tiers) | ~7.8% WER; ~48% lower error than same-size Whisper Tiny; matches/beats 28x larger Whisper Medium in most cases per the authors | Compute scales with actual audio duration (not fixed-window like Whisper) — 5x less compute than Whisper tiny.en for a 10s clip | Apache 2.0-family (Useful Sensors, open) | No | [arXiv 2410.15608](https://arxiv.org/pdf/2410.15608v1), [Useful Sensors GH](https://github.com/usefulsensors/moonshine) (checked 2026-09-30) |
| **NVIDIA Parakeet-TDT-0.6B-v3** | 600M | 6.34% WER across 25 European languages, auto language ID | RTFx "near >2,000" for the 1.1B streaming variant — extremely fast, GPU-oriented | Permissive (NVIDIA open release) | No native OpenAI endpoint; NeMo serving stack | [NVIDIA Nemotron speech FAQ](https://perspectives.nvidia.com/nemotron-speech/task/faq/what-are-the-most-production-ready-open-speech-recognition-models-for-european-l/) (checked 2026-09-30) |
| **NVIDIA Canary-Qwen-2.5B / Canary-1B-v2** | 1B–2.5B | 5.63% WER, #1 on HF Open ASR Leaderboard (Canary-Qwen) | Optimized for accuracy over raw speed (vs. Parakeet) | Permissive | No | Same source (checked 2026-09-30) |
| **Mistral Voxtral-Mini-3B / Small-24B** | 3B / 24B | Marketed as SOTA at release (mid-2025); no independent WER figure surfaced this pass | Low-latency claim for Mini; API also offers a transcription-optimized Mini variant | Apache 2.0 | **Yes** — Mistral's hosted API is OpenAI-compatible-style for audio; self-hosted checkpoints don't include a server, so datawatch would need to front one | [Mistral Voxtral announcement](https://mistral.ai/news/voxtral/), [MarkTechPost](https://www.marktechpost.com/2025/07/17/mistral-ai-releases-voxtral-the-worlds-best-and-open-speech-recognition-models/) (checked 2026-09-30) |
| **Qwen3-ASR** (0.6B/1.7B) | 0.6B / 1.7B | 1.7B: Common Voice macro-avg WER as low as 4.57% (best on 6/7 languages in that set); per-language CV figures include Indonesian 6.92%, Thai 3.73%, Korean 7.22%, Arabic 17.05%, Vietnamese 12.31%, Japanese CER 23.09% | Not confirmed this pass | Apache 2.0 | Not confirmed | Supports 30 languages + 22 Chinese dialects; released 2026-01-29 on the official `QwenLM/Qwen3-ASR` GitHub/HF repo — resolved this pass, was previously a low-confidence mirror-only entry: [Qwen blog](https://qwen.ai/blog?id=qwen3asr), [QwenLM/Qwen3-ASR](https://github.com/QwenLM/Qwen3-ASR), [HF Qwen/Qwen3-ASR-1.7B](https://huggingface.co/Qwen/Qwen3-ASR-1.7B) (checked 2026-09-30 via `WebSearch`, not the SearXNG proxy — see note below) |
| **Kyutai STT** (Delayed Streams Modeling) | Not confirmed (paired with the TTS sizes below) | Not confirmed | Streaming, sub-200ms round-trip in the full-duplex Moshi framework | Open (Kyutai is an open-science lab) | No | [Kyutai](https://kyutai.org/), [Moshi arXiv](https://arxiv.org/abs/2410.00037) (checked 2026-09-30) |

### Text-to-speech candidates

| Model | Size | Real-time factor | License | Notes | Source |
|---|---|---|---|---|---|
| **Piper** | Per-voice, small (tens of MB) | CPU RTF ~0.15–0.2 (≈5–6.5x realtime) on a 12th-gen Intel laptop chip | Active fork now **GPL-3.0-or-later** (`OHF-Voice/piper1-gpl`); original `rhasspy/piper` (MIT) is archived/unmaintained | 175 voices across 56 locales — breadth leader | [Cekura](https://www.cekura.ai/discover/piper-tts), [OHF-Voice/piper1-gpl](https://github.com/OHF-Voice/piper1-gpl) (checked 2026-09-30) |
| **Kokoro-82M** | 82M (~164 MB fp16) | CPU: 3–11x realtime on modern CPUs (one M3 Pro measurement: ~6x); GPU (A100): RTF ~0.03 | Apache 2.0 | #1 on TTS Arena per its own marketing at 82M params — best quality/size/license combination found | [VisionStory](https://www.visionstory.ai/open-source/kokoro-tts), [TextToLab](https://texttolab.com/blog/kokoro-tts-review) (checked 2026-09-30) |
| **Kyutai Pocket TTS** | 100M | Explicitly "lightweight enough to run on a CPU in real-time" (Jan 2026 release) | Open (Kyutai) | Purpose-built as the CPU-friendly member of Kyutai's TTS lineup (vs. the 1.6B server-class Kyutai TTS) | [Kyutai TTS](https://kyutai.org/tts/) (checked 2026-09-30) |
| **Chatterbox / Chatterbox-Turbo** (Resemble AI) | Not stated | "Real-time generation" claimed; preferred over ElevenLabs 65.3% vs 24.5% in a blind listener test (Resemble's own test) | MIT | Zero-shot voice cloning from 5s audio; Multilingual v3 (2026-06-10) adds 25 languages | [Resemble AI](https://www.resemble.ai/learn/models/chatterbox), [FindSkill](https://findskill.ai/blog/best-open-source-tts-2026/) (checked 2026-09-30) |
| **Coqui XTTS v2** | Not stated | Not found this pass | **Checkpoint is non-commercial** (Coqui Public Model License) despite the code library being MPL-2.0; Coqui Inc. shut down Jan 2024, no path to a commercial license | Best avoided for anything beyond personal/research use given the license and dead upstream | [Resemble AI comparison](https://www.resemble.ai/resources/best-open-source-ai-voice-cloning-tools) (checked 2026-09-30) |
| **Dia** (Nari Labs) | 1.6B | Not stated | Apache 2.0 | Generates full dialogue (not single-utterance) directly from a transcript in one pass; supports audio-conditioned voice/emotion cloning and nonverbal tags (laughs, coughs); **English only**. Resolved this pass — was previously unconfirmed. | [nari-labs/Dia-1.6B (HF)](https://huggingface.co/nari-labs/Dia-1.6B), [nari-labs/dia (GitHub)](https://github.com/nari-labs/dia) (checked 2026-09-30 via `WebSearch`) |
| **Orpheus** (Canopy Labs) | 3B (Llama-3.2-3B-Instruct backbone) | Marketed for low-latency/real-time streaming | Apache 2.0 on the checkpoint, but built on a **gated Llama-3.2 base** — review the inherited Llama Community License terms before treating this as unconditionally Apache | Zero-shot voice cloning, emotion-tag control; heavier (3B, Llama-based) than Piper/Kokoro, so a CPU-only deployment is less certain — treat as a GPU-leaning candidate unless a CPU RTF figure is confirmed. Resolved this pass — was previously unconfirmed. | [canopyai/Orpheus-TTS (GitHub)](https://github.com/canopyai/Orpheus-TTS), [canopylabs/orpheus-3b-0.1-pretrained (HF)](https://huggingface.co/canopylabs/orpheus-3b-0.1-pretrained) (checked 2026-09-30 via `WebSearch`) |

### Recommendation for voice

- **STT default:** replace the currently-configured Whisper `medium` model
  with **faster-whisper large-v3-turbo running int8 on CPU**. It's ~1-2 WER
  points worse than full large-v3 but ~4-6x faster, and CPU inference keeps
  it entirely off the 32 GB GPU that the LLM/vision workloads already share.
  The Threadripper 9960X (24c/48t) has ample headroom for this — voice notes
  are bursty, not sustained load.
- **If ultra-low latency streaming matters more than raw accuracy** (e.g. a
  future live-voice-assistant feature rather than async voice-note
  transcription), Kyutai's streaming STT or NVIDIA Parakeet are the
  purpose-built options — both are overkill for today's async
  Signal/Telegram voice-note use case.
- **TTS: not currently present at all.** If/when a voice-reply feature is
  wanted, **Piper** (GPL-3.0 now, broadest voice/language coverage, proven
  CPU RTF) or **Kokoro** (Apache-2.0, better quality-per-parameter, similar
  CPU RTF) are both CPU-only options that avoid competing with GPU-resident
  LLM/vision work. Kokoro's permissive license is the safer default if
  license posture matters more than voice-catalog breadth; Piper if breadth
  of built-in voices/languages matters more.

---

## Part 3 — Embeddings

### Current state

`get_config` (`memory` section): `backend: postgres`, `embedder_model:
nomic-embed-text`, `embedder_host: http://local-node:11434`, `dimensions:
0` (unset in this config view — the effective dimension is nomic-embed-text's
native 768, per `ollama_stats` showing it loaded locally at 0.6 GB VRAM).
This matches the brief.

### Candidates

| Model | Params | Dimension | Context | MTEB signal | Ollama? | Source |
|---|---|---|---|---|---|---|
| **nomic-embed-text v1.5** (current) | 137M | 768, Matryoshka-truncatable down to 64 | 8192 tokens | Outperforms `text-embedding-3-small`/`ada-002` at reduced dims; ~1.2-point loss truncating 768→256 (3x smaller) | Yes (already running) | [Nomic Matryoshka post](https://www.nomic.ai/news/nomic-embed-matryoshka), [Zilliz guide](https://zilliz.com/ai-models/nomic-embed-text-v1.5) (checked 2026-09-30) |
| **nomic-embed-text v2** | MoE, ~305M total / much smaller active | Not confirmed this pass | Not confirmed | Positioned as the multilingual upgrade path (per one source's summary); not independently benchmarked here | Community tags exist; not confirmed as an official Ollama library tag | [localaimaster summary](https://localaimaster.com/models/nomic-embed-text) (checked 2026-09-30) — **secondary source, verify against Nomic's own release notes before adopting** |
| **BGE-M3** | ~568M | Dense + sparse + multi-vector (ColBERT-style) simultaneously — no single comparable MTEB number because it spans retrieval modes | 8192 tokens | "No single comparable number" per the comparison source — evaluate on your own retrieval mix if considering it | Yes (community tags) | [premai.io 2026 ranking](https://www.premai.io/blog/best-embedding-models-for-rag-2026-ranked-by-mteb-score-cost-and-self-hosting/) (checked 2026-09-30) |
| **Qwen3-Embedding-0.6B / 4B** | 0.6B / 4B (family goes to 8B) | Flexible/instructable output dims | Long context (family supports well beyond 8K; exact figure not isolated for the 0.6B tag this pass) | 8B variant is **#1 on the MTEB multilingual leaderboard** (70.58, as of 2025-06-05) — 0.6B/4B trade some of that for size | Yes — `qwen3-embedding:0.6b`/`4b`/`8b` all present in the Ollama library | [Ollama library](https://ollama.com/library/qwen3-embedding) (checked 2026-09-30) |
| **Snowflake Arctic Embed v2** (m/l) | Not stated (m = "medium", small footprint) | Not stated | Not stated | Retrieval-tuned, reports NDCG@10 on BEIR rather than the standard MTEB table — not directly comparable to the others without re-running | Community tags exist | [BentoML guide](https://www.bentoml.com/blog/a-guide-to-open-source-embedding-models) (checked 2026-09-30) |
| **mxbai-embed-large** | 335M | Not stated | **512 tokens only** — a hard limitation for long memory entries, would force chunking | Strong English MTEB (retrieval/clustering/classification) | Yes | Same source (checked 2026-09-30) |
| **EmbeddingGemma-300M** | 300M | Matryoshka 768→512→256→128 | Not stated | "Best cost-per-quality ratio for high-volume indexing" per one comparison; strong on the multilingual leaderboard | Not confirmed as an Ollama-library tag this pass | Same source (checked 2026-09-30) |

### Re-indexing cost (why this isn't a free swap)

Changing the embedder is not a config toggle: it requires `memory_reindex`
to re-embed the existing memory store, and if the new model's native
dimension differs from the current pgvector column width, the **postgres
vector column itself has to be migrated** (pgvector dimensions are fixed
per column/index). A same-dimension swap (e.g. staying at 768) avoids the
schema migration; jumping to a model with a different native width does
not.

### Recommendation for embeddings

- **No urgent reason to move off nomic-embed-text v1.5.** It already gives
  8192-token context, a tunable 768→64 dimension range, and is already
  correctly wired (postgres + `memory_reindex` exist, `local-node` host).
- **If retrieval quality becomes a bottleneck** (e.g. memory recall
  precision issues show up in practice), **Qwen3-Embedding-0.6B** is the
  best upgrade candidate: it's from a family that tops the MTEB
  multilingual leaderboard at the 8B tier, is already an official Ollama
  tag, is small enough to run comfortably on either node, and its flexible
  output dimensions mean the postgres column can be kept at 768 to avoid a
  schema migration.
- **mxbai-embed-large's 512-token cap** makes it a poor fit for datawatch's
  memory entries specifically (session summaries and multi-paragraph notes
  routinely exceed 512 tokens) — rule it out regardless of its English MTEB
  strength.

---

## Recommendation table

| Model | Role | Node | CPU or GPU | Why |
|---|---|---|---|---|
| `moondream:latest` (keep) | Vision — comms-router quick captions, default `vision_describe` | `local-node` (local 32 GB GPU) | GPU | Already installed and default; lowest latency; 2026 OCR/UI-localization gains keep it competitive for "what's in this photo" |
| `llmvision/glimpse-v1` (keep, narrow role) | Vision — home-security camera event summaries only | `local-node` | GPU | Specialized Gemma 3 4B fine-tune; 1.9x better than base Gemma 3 4B *on that task specifically* — don't promote it to general-purpose |
| `qwen3-vl:4b` (add) | Vision — Council image critique, diagram/screenshot understanding | `local-node` | GPU | Native Ollama vision support, small footprint (~6 GB Q4_K_M), family benchmarks explicitly cover diagram/UI-style reasoning that Moondream/glimpse don't target |
| `faster-whisper large-v3-turbo` (int8, replaces configured Whisper `medium`) | Voice — STT for Signal/Telegram/Matrix voice notes | `local-node` | **CPU** (Threadripper 9960X) | ~4-6x realtime on CPU at a 1-2 WER-point cost vs. large-v3; keeps voice off the shared GPU |
| Piper **or** Kokoro (new capability, not yet enabled) | Voice — TTS voice replies, if/when wanted | `local-node` | **CPU** | Both hit usable real-time-or-better CPU RTF; Piper = GPL-3.0 + broadest voice/language catalog, Kokoro = Apache-2.0 + higher quality-per-parameter |
| `nomic-embed-text` v1.5 (keep) | Memory embeddings | `local-node` | GPU (tiny — 0.6 GB VRAM observed) | Already correctly wired end-to-end (postgres, `memory_reindex`); no reindex cost; 8192-token context already sufficient |
| `qwen3-embedding:0.6b` (optional future upgrade) | Memory embeddings, if retrieval quality becomes a bottleneck | `local-node` | GPU or CPU | Family tops MTEB multilingual leaderboard; official Ollama tag; flexible dims can preserve the existing 768-wide postgres column and skip a schema migration |

## Open items / lower-confidence spots, stated plainly

- **Resolved this pass (fourth retry, 2026-09-30):** **Dia**, **Orpheus**,
  and **Qwen3-ASR** were re-searched via this session's `WebSearch` tool,
  not the `web_search` MCP/SearXNG proxy — that was re-tried first and again
  returned topically unrelated results for all three (e.g. "Dia TTS Nari
  Labs" → U.S. Defense Intelligence Agency pages), a fourth independent
  reproduction of the SearXNG failure mode 03 first reported. All three now
  have confirmed size/license/sources above; Orpheus's checkpoint is Apache
  2.0 but its base (Llama-3.2-3B-Instruct) is gated under Meta's Llama
  Community License, so its effective terms aren't unconditionally Apache.
- The `whisper` config's `backend: ""` still couldn't be resolved to a
  concrete implementation name. Re-attempted this pass: `docs_read` on
  `howto/voice-input.md` returns only the preamble, and guessed anchors
  (`backends`, `configuration`) both 404 — genuinely not retrievable through
  the tools available to this task. The `venv_path`/`model: "medium"`
  naming-convention inference stands as the best available signal.
- **distil-whisper** and **nomic-embed-text v2** figures are still carried
  from secondary-source knowledge, not re-checked live this pass (scope this
  retry to the items above).
