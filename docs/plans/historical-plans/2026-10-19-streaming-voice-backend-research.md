# Streaming / near-live voice backend research

Date: 2026-10-19
Status: research note (no code changes). Recovered 2026-11-26 from a branch that was never
merged; model versions and the description of the transcriber are those of 2026-10-19 and were
not rechecked.
Scope: speech-to-text backends for **live/near-live transcription** on datawatch's
deployment class — a single operator-run daemon, **CPU-only as the baseline**.

## Baseline: what we'd be replacing/augmenting

`internal/transcribe/transcribe.go` today runs, per file:

```python
model = whisper.load_model(model_name, device="cpu")   # model = "medium"
result = model.transcribe(audio_path, fp16=False, language=...)
```

i.e. openai-whisper (Python venv), model `medium`, batch per-file, no incremental
output. Measured on this host: ~1.5× realtime (a ~348 s run for a few minutes of
audio is recorded in the code comments around `DefaultTimeout`), and the 20-minute
subprocess timeout exists precisely because medium is slow on CPU. Everything below
is judged against that baseline.

Legend: **[doc]** = from the project's official docs/HF card/repo; **[inferred]** =
my estimate or extrapolation, not a documented figure.

---

## 1) NVIDIA Parakeet TDT (FastConformer-TDT)

- **Versions** **[doc]**: `parakeet-tdt-0.6b-v2` — 600M params, English only,
  full-attention over up to ~24 min of context, licensed **CC-BY-4.0** (permissive).
  `parakeet-tdt-1.1b-v3` (v3 family) extends to ~25 European languages.
- **Architecture** **[doc]**: FastConformer **transducer (TDT)** — the
  encoder–joiner–decoder split is what makes transducers natively
  cache-friendly, which is why NVIDIA ships it as the *streaming-capable* family.
- **Size / memory** **[inferred]**: 600M params → ~1.2 GB at fp32, ~0.6 GB fp16/
  int8-ish. Comparable to whisper medium (769M), slightly smaller; roughly
  half of whisper large.
- **Language support** **[doc]**: v2 = English; v3 = ~25 EU languages.
- **How streaming works** **[doc]**: NeMo's `asr_streaming_infer.py` supports
  buffered CTC, RNNT **and TDT** streaming. Important nuance from the NeMo docs:
  only CTC and RNNT are *cache-aware* (true incremental token streaming); **TDT
  streaming is buffered/near-live** — the model emits hypotheses as buffers flush,
  not per-token.
- **Latency class on CPU** **[inferred]**: transducers are among the
  lightest modern ASR per parameter. For a 5–10 s buffer on a modern 4–8-core
  x86: TTFT on the order of seconds, steady-state RTF plausibly 0.1–0.5. I have
  not benchmarked Parakeet on this host's CPU — treat numbers as an order of
  magnitude, not measurements. Accuracy per NeMo/NVIDIA claims puts 0.6B
  competitive with models an order of magnitude larger on clean speech **[doc]**.
- **"Transcript appears while talking"**: chunked near-live (buffer flushes every
  ~chunk period). Not token-streaming, same class as Moonshine and chunked
  whisper.

## 2) Moonshine (Useful Sensors)

- **License** **[doc]**: MIT.
- **Sizes** **[doc]**: streaming models at ~34M / 123M / 245M params — i.e.
  ~3–20× smaller than whisper medium.
- **Accuracy** **[doc]**: reported Medium streaming WER ~6.65%, competitive with —
  and per the docs better than — whisper large-v3 (7.44) on their benchmark.
  English-centric quality; multilingual coverage narrower than whisper/Parakeet-v3
  **[inferred from model lineup — verify per-version HF page before relying on it]**.
- **How streaming works** **[doc]**: `OnnxRuntime` core with an `add_audio(...)`
  API — you push audio chunks and the engine re-decodes on a **500 ms update
  interval**. Output = updated hypothesis text per update cycle. Chunked near-live,
  not token-streaming.
- **Latency class on CPU** **[doc]**: post-phrase latencies on Linux x86:
  ~69 / ~165 / ~269 ms (across the three sizes). This is *post-phrase* (time from
  silence to stable text), not TTFT on ongoing speech, but it indicates the model
  is comfortably CPU-realtime at all sizes.
- **Memory** **[inferred]**: tens of MB (34M→~100MB, 245M→~1GB) vs whisper
  medium ~1.5 GB+. The lightest option by far.
- **Fit**: smallest runtime, best TTFT, but smallest model and — the caveat —
  English-first. If datawatch transcripts are mostly operator speech in English,
  this is the strongest raw fit.

## 3) Chunked / incremental whisper (no-new-model fallback)

Reuse the exact existing script and venv; the only change is *how audio is fed*.

- **Approach A — whisper's own `chunk_length`** **[doc]**: `model.transcribe(...,
  chunk_length=N)` splits audio into N-second segments sequentially. It's an
  offline segmentation parameter — you still get text per chunk only if you
  restructure the call, and it offers no TTFT improvement over waiting for the
  whole file unless you drive it per-chunk ourselves.
- **Approach B — whisperX-style VAD chunking** **[inferred design]**: VAD
  (Silero/energy), transcribe each voice segment with the *same* medium model.
  Per-segment the model is still batch, but you emit each segment's text as soon
  as it's done.
- **TTFT / steady state** **[inferred]**: medium on this host runs ~1.5× realtime
  (from the `DefaultTimeout` comments). So a 10 s voice chunk appears after ~10–15 s;
  a 30 s chunk after ~30–45 s. TTFT ≈ chunk length × 1.5. Comfortable near-live
  requires small chunks (≤10 s), at which point the per-chunk cost is ~10–15 s —
  **this is the weakest TTFT of the four options on this CPU**.
- **Memory** **[doc-equivalent]**: identical to today — whisper medium ~1.5 GB+.
- **Pros/cons**: zero new dependencies, zero license questions, identical model
  quality to today. Con: slowest TTFT, and per-chunk model warm-up/context loss
  (whisper has no cross-chunk state) can hurt quality at chunk boundaries
  **[inferred]**.

## 4) OpenAI-compatible HTTP streaming endpoint

The `internal/transcribe/openai_compat.go` path already knows how to talk to an
OpenAI-compatible audio server; adding a streaming transport is the natural
extension when an operator runs one.

- **Candidate servers** **[doc]**: `whisperX` server and `whisper.cpp`
  (`whisper-stream`, ~500 ms step) both expose incremental/chunked output;
  `faster-whisper-server` and similar wrappers expose the standard
  `/v1/audio/transcriptions` endpoint.
- **Important API nuance** **[doc]**: the standard OpenAI
  `POST /v1/audio/transcriptions` endpoint is **non-streaming** (file in, full
  text out). True "streaming" in the OpenAI-compatible ecosystem for ASR is
  chunk-based: the server accepts an upload or a chunk and returns progressively
  (e.g. whisper.cpp's 500 ms-step stream, whisperX per-segment output). None of
  the common open servers implement per-token SSE from ASR today **[inferred
  from surveyed server READMEs — recheck specific server before building on it]**.
- **TTFT / steady state** **[inferred]**: inherits whatever model the server runs.
  On a CPU server with whisper-medium: same ~1.5× RTF as §3 but you avoid process
  spawn + model load per call (load once). With faster-whisper (CTC-free CTranslate2)
  or Parakeet on the server, RTF drops to ~0.2–0.5 even on CPU for small buffers.
- **Fit**: best for the "operator already runs a model host" scenario (see
  `docs/plans/2026-10-18-model-host-requirements.md`); weakest for the
  CPU-only-single-daemon baseline because it adds a dependency.

---

## Comparison (CPU baseline, per ~10 s of audio)

| Option | Size | TTFT (CPU, 10 s chunk) | Steady-state RTF | Token-streaming? | License |
|---|---|---|---|---|---|
| Parakeet TDT 0.6B | ~0.6–1.2 GB | seconds **[inferred]** | ~0.1–0.5 **[inferred]** | no — buffered near-live | CC-BY-4.0 |
| Moonshine 245M | ~0.2–0.5 GB | ~500 ms updates **[doc: 500 ms interval; 69–269 ms post-phrase]** | very low | no — 500 ms update cycle | MIT |
| Chunked whisper medium | ~1.5 GB | ~10–15 s **[inferred from measured RTF]** | ~1.5 (measured here) | no — per-chunk batch | MIT |
| OpenAI-compat server | depends | depends on server model | depends | no (chunked, e.g. 500 ms) | MIT/CC-BY depending |

Memory note: "whisper medium" ≈ 769M params ≈ ~1.5 GB fp32 in the current venv
path **[inferred]**; Parakeet 0.6B and Moonshine ≤245M are equal-to-well below
that.

## Shortlist & recommendation

1. **Moonshine (MIT, ≤245M)** — best fit for the CPU-only single-daemon baseline:
   smallest, fastest to first update (~500 ms cadence), MIT-licensed, and the docs
   claim large-v3-class accuracy. Watch: English-centric — confirm the specific
   version's language list covers what operators will say before relying on it.
2. **NVIDIA Parakeet TDT 0.6B-v2 (CC-BY-4.0)** — the higher-ceiling English
   option: same memory class as whisper medium but transducer architecture,
   documented streaming support in NeMo, and per NVIDIA docs much better
   WER/memory ratio than whisper medium. Choose this if we're happy to depend
   on NeMo/Triton or a NeMo-based inference wrapper rather than Moonshine's
   standalone OnnxRuntime runtime.

**Hardware tier for RTF < 1 (comfortable real-time):**
Moonshine → any modern 4-core x86 CPU is comfortably above RTF 1, and even a
single strong core at the 34M size. Parakeet TDT 0.6B → modern 4–8-core CPU
at fp16/int8, or an entry GPU (T4-class / ~4 GB) if we want headroom for
longer buffers or concurrent streams. Whisper-medium chunked → needs either an
entry GPU or a much higher-end CPU (16+ cores) to get TTFT under ~5 s; on
this host's CPU it's ~1.5× realtime, i.e. RTF < 1 but not *comfortably*.

## Open questions (not blocking, check before implementation)

- Confirm Moonshine version's supported-language list covers operator languages.
- Re-verify the exact streaming endpoint/SDK shape for the chosen OpenAI-compatible
  server (whisper.cpp server / faster-whisper-server / whisperX) — the "streaming"
  surface is chunk-based, not SSE-token-based, and differs per server.
- Benchmark Parakeet TDT 0.6B on this exact host's CPU before committing to it as
  the RTF < 1 baseline; all Parakeet RTF figures above are inferences, not
  measurements.
