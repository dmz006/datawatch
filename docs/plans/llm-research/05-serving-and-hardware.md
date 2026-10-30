# 05 — Serving and Hardware

**Goal:** explain concretely how to run 8+ concurrent LLM calls across
datawatch's two GPU nodes and the CPU with balanced utilization, using real
numbers from the live registry rather than vendor marketing figures.

**Verification note:** the brief's "environment facts" (captured 2026-09-29)
were re-checked live via MCP tools and `/api/show` on 2026-09-30. Almost all
confirmed exactly. Three corrections:

1. **Voice pipeline exists.** The brief says no `voice` section exists in
   `get_config`. Live `get_config` shows `whisper: {"enabled":true,
   "language":"en","model":"medium","backend":""}` — a real, enabled local
   Whisper pipeline (implementation unclear, `backend` is blank). Relevant to
   §c: Whisper-medium is a good CPU tenant.
2. **datawatch version moved**: brief said 8.34.1, live `get_version` on
   2026-09-30 reports **8.36.7** (with an update-checker oddity claiming
   "8.36.6 available" while running 8.36.7 — likely a comparator bug, not
   pursued here).
3. **Autonomous/orchestrator numbers drifted**: live `max_parallel_tasks: 0`
   (brief said 3), `orchestrator.max_parallel_prds: 0` (brief said 2;
   `enabled: false` matches). `verification_backend` is `ollama-datawatch`.

Everything else in the brief (node registry, model inventory, LLM registry,
council config, capacity ledger, all-zero cost tracking) was independently
confirmed live and is restated below only where needed for the math.

---

## (a) Ollama concurrency internals (server 0.33.2 on `datawatch`)

Concurrency is controlled by env vars read at startup
([Ollama FAQ](https://docs.ollama.com/faq), checked 2026-09-30;
[envconfig source](https://github.com/ollama/ollama/blob/main/envconfig/config.go), checked 2026-09-30):

| Variable | Default | Effect |
|---|---|---|
| `OLLAMA_NUM_PARALLEL` | `1` | Max simultaneous requests *per loaded model*. Each slot gets its own KV cache — memory scales as `NUM_PARALLEL × num_ctx`. |
| `OLLAMA_MAX_LOADED_MODELS` | `3 × GPU count` (`3` for CPU-only) | How many distinct models can be resident at once, memory permitting. |
| `OLLAMA_KEEP_ALIVE` | `5m` | How long an idle model stays loaded. Negative = forever, `0` = unload after each request. |
| `OLLAMA_FLASH_ATTENTION` | unset (auto) | Three-state since the 2025-10-01 change: unset = auto-decide per backend/device, `1`/`0` force on/off. |
| `OLLAMA_KV_CACHE_TYPE` | `f16` | Server-wide KV quantization. `q8_0` ≈ half the memory of `f16`, small quality loss; `q4_0` more aggressive/lossier. Can't mix precisions per-model on one server. |
| `OLLAMA_MAX_QUEUE` | `512` | Requests beyond in-flight capacity queue to this depth, then `503`. |
| `num_ctx` (per-request) | model default | Context window; combined with `NUM_PARALLEL` is the other KV VRAM multiplier: `VRAM_kv ≈ NUM_PARALLEL × num_ctx × kv_bytes/token` (§e has worked numbers). |

**GPU/CPU layer split (`num_gpu`):** Ollama estimates each layer's memory
footprint and greedily assigns layers to GPU (working from the output layers
inward) until VRAM runs out, leaving the rest on CPU
([VRAM guide](https://localllm.in/blog/ollama-vram-requirements-for-local-llms);
[GPU scheduling writeup](https://eastondev.com/blog/en/posts/ai/ollama-gpu-scheduling/), both checked 2026-09-30).
The default `num_gpu: 99` means "fit as many as possible," auto-capped at
the model's real layer count — not a literal request for 99 layers. The
estimate is occasionally wrong at the margin (model barely fits), in which
case `num_gpu` is pinned manually via the API `options` field.

**Live observation:** at brief time `qwen3.8:27b` (17.7 GB disk, Q4_K_M) was
loaded using only 15.5 GB VRAM — ~2 GB already pushed to CPU, likely
parallel-slot overhead or a co-resident model, not a fit failure on a
128 GB card. "Declared VRAM" and "VRAM free right now" are different numbers
on a multi-model node.

**Registry gap (resurfaces in §f):** the `datawatch` node (`kind: ollama`,
port 11434) has **no `declared_capacity`**, while the sibling
`datawatch-openai` node (`kind: openai-compat`, port 3000, same host)
declares the B200. If both front the same physical GPU — plausible, same
host — capacity admission currently can't see that relationship, and both
nodes could schedule onto the same card unaware of each other.

---

## (b) Alternatives to Ollama

| Engine | Concurrency model | Quant formats | OpenAI-compatible | Complexity |
|---|---|---|---|---|
| **Ollama** | Fixed parallel slots, simple queue | GGUF | Yes (`/v1`) | Lowest |
| **llama.cpp `llama-server`** | Continuous batching, `--n-gpu-layers`, `--override-tensor`/`--n-cpu-moe` for MoE-on-CPU, optional speculative decoding | GGUF | Yes (`tools/server`) | Low-medium, many knobs |
| **vLLM** | PagedAttention (block-based KV, near-zero fragmentation) + continuous batching + prefix caching | AWQ, GPTQ, FP8, **NVFP4** (Blackwell, since Aug 2026) | Yes | Medium — Python service, driver-sensitive |
| **SGLang** | RadixAttention (prefix-tree KV reuse) + continuous batching | FP8, NVFP4 | Yes | Medium, similar to vLLM |
| **LM Studio** | Added continuous batching (llama.cpp parallel-slot) in its 0.4.0 line; concurrency is secondary to Ollama/vLLM | GGUF, MLX | Yes | Low, GUI-first |
| **llamafile** | Single-request-oriented, not built for concurrency | GGUF | Yes (minimal) | Lowest, best for portability not throughput |

Sources: [LM Studio vs Ollama](https://www.betterclaw.io/blog/ollama-vs-lm-studio),
[server comparison](https://cohorte.co/blog/local-llm-openai-compatible-server),
[vLLM Blackwell blog](https://blog.vllm.ai/2026/02/03/dsr1-gb200-part1.html),
[NVFP4 explainer](https://packet.ai/blog/nvfp4),
[SGLang vs vLLM 2026](https://www.spheron.network/blog/vllm-vs-sglang-2026/) (all checked 2026-09-30).

**Throughput vs. concurrency** (no single apples-to-apples benchmark found
across all five engines on identical hardware — treat shape, not exact
numbers, as the takeaway): at **concurrency 1** engines are roughly
comparable (bandwidth/compute bound, not batching-clever). By
**concurrency 4–8**, PagedAttention/RadixAttention engines pull ahead —
one published vLLM run shows output throughput going from **129 tok/s at
concurrency 1 to 1,444 tok/s at concurrency 32**, a ~2.8× per-request
efficiency gain from batching alone
([source](https://arxiv.org/pdf/2511.17593), checked 2026-09-30). At
**16+**, the gap is largest: vLLM keeps scaling toward saturation while
fixed-slot servers hit `NUM_PARALLEL` and start queuing. **For datawatch:**
at the council/PRD concurrency levels this project targets (8–16), a
PagedAttention engine on the B200 would likely out-throughput Ollama — and
`datawatch-openai` already exists as an `openai-compat` node specifically to
make that swap possible without touching LLM registry call sites (§f).

**Quant formats:** GGUF (Ollama/llama.cpp) supports the widest quant range
and is easiest for mixed CPU/GPU since llama.cpp partially offloads GGUF
tensors to CPU. AWQ/GPTQ are GPU-only formats vLLM/SGLang favor for
GPU-only throughput. NVFP4 gets a 70B model to ~40 GB vs ~70 GB at FP8
([source](https://packet.ai/blog/nvfp4), checked 2026-09-30) — meaningful on
the B200 (bigger models or more parallel slots in 128 GB), not really
needed on the 32 GB GPU where GGUF Q4/Q8 already fits comfortably.

---

## (c) CPU inference on the Threadripper 9960X

Confirmed live (`lscpu`/`free -h`, 2026-09-30): **24c/48t, single socket,
one NUMA node, 122 GiB RAM** (93 GiB free). AMD/retailer specs: **quad-channel
DDR5-6400, ~204.8 GB/s theoretical bandwidth**
([AMD product page](https://www.amd.com/en/products/processors/ryzen-threadripper/9000-series/amd-ryzen-threadripper-9960x.html),
[cpu-monkey](https://www.cpu-monkey.com/en/cpu-amd_ryzen_threadripper_9960x), both checked 2026-09-30).
Zen 5 doubled the AVX-512 datapath to native 512-bit on this part
([Zen 5 — Wikipedia](https://en.wikipedia.org/wiki/Zen_5), checked 2026-09-30),
which is what makes CPU GGUF inference viable at all vs. older AVX2-only Zen.

**No published benchmark for this exact CPU was found.** What follows is a
bandwidth-bound estimate: `tok/s ≈ effective_bandwidth / (active_params ×
bytes_per_param)`, using ~140 GB/s as ~65-70% of theoretical peak:

| Model class | Active params | Active weight bytes | Estimated ceiling |
|---|---|---|---|
| Dense 8B (`qwen3:8b`, 5.2 GB disk) | ~8B | ~5.2 GB | ~**27 tok/s** |
| Dense 4B class | ~4B | ~2.6 GB | ~**54 tok/s** |
| MoE 30B-A3B class | ~3B active | ~2 GB | ~**70 tok/s** |
| gpt-oss-20b class | ~3.6B active | ~2.2 GB | ~**64 tok/s** |

Anecdotal calibration points (**different CPUs**, not the 9960X): a Ryzen 7
5700X reportedly hits ~60 tok/s on an unspecified model, and one llama.cpp
discussion calls gpt-oss-20b "CPU-bound... 30 tok/s" on an unstated CPU
([discussion #15396](https://github.com/ggml-org/llama.cpp/discussions/15396), checked 2026-09-30) —
same order of magnitude as the table, but **not a verified benchmark for
this hardware**; prefill speed is separate and typically much higher.

**Why MoE is CPU-friendly:** the formula depends on *active*, not total,
params — a 30B-A3B model streams the same ~2 GB/token as a 3B dense model
despite sitting in ~20 GB of RAM. One of the few contexts where
bigger-but-sparser isn't slower than smaller-dense.

**NUMA/pinning:** generic dual-socket EPYC/Xeon NUMA-pinning advice **does
not apply** — `lscpu` confirms single-socket/single-NUMA-node here
([NUMA discussion](https://github.com/ggml-org/llama.cpp/discussions/12303), checked 2026-09-30,
notes this tuning is dual-socket-specific and can hurt single-socket boxes).
What does matter: thread count (saturates well below 48 — "~3 cores per
memory channel" is a common rule of thumb, suggesting `--threads` ~12–16 is
worth testing against the default;
[source](https://github.com/ggml-org/llama.cpp/discussions/19102), checked 2026-09-30);
SMT (hyperthreads mostly compete for the same memory controller on a
bandwidth-bound load — benchmark 24 vs 48 threads); batch size (`-b`/`-ub`
mainly helps prefill/concurrent-CPU-request throughput, not single-stream
generation).

**Worth it on this fleet:** embeddings (`nomic-embed-text`, 137M params,
already the configured memory embedder at `http://local-node:11434`);
speech (the confirmed `whisper: medium` pipeline); guardrail/verifier
classification (`verification_backend: ollama-datawatch` today — small
low-latency classification is exactly the bandwidth-bound case above, and
moving it off-GPU frees generation slots); small council personas
(`qwen3:1.7b`-class, not all 12). **Not worth it:** dense models ≥8–14B
active params, single-digit-second SLAs, or anything needing 8+ concurrency
from one model — CPU has no `NUM_PARALLEL`-equivalent free lunch.

---

## (d) CPU+GPU split: `gpt-oss:120b` and `qwen3-coder-next`

Both are MoE with small active fractions, confirmed via `/api/show`:

- **`gpt-oss:120b`**: 116.8B total, 36 layers, publicly ~5.1B active
  params/token ([OpenAI gpt-oss announcement](https://openai.com/index/introducing-gpt-oss/), checked 2026-09-30).
  `sliding_window: 128` — alternates windowed/full attention, so KV cache is
  smaller than naive full-attention math suggests (§e).
- **`qwen3-coder-next:q4_K_M`**: 79.7B total, 48 layers, `head_count_kv:
  None` in `/api/show` — not standard multi-head attention. Matches
  **Qwen3-Next-80B-A3B**'s public architecture: hybrid Gated DeltaNet +
  Gated Attention, **3B active/token**, full attention on ~1-in-4 layers
  (`full_attention_interval: 4`, confirmed), linear-attention state (not a
  KV cache) on the rest
  ([NVIDIA technical blog](https://developer.nvidia.com/blog/new-open-source-qwen3-next-models-preview-hybrid-moe-architecture-delivering-improved-accuracy-and-accelerated-parallel-processing-across-nvidia-platform/), checked 2026-09-30).

**Technique:** llama.cpp `--n-cpu-moe N` or `--override-tensor` pins the
huge sparsely-activated expert matrices to system RAM while attention/shared
weights stay on GPU ([offload guide](https://huggingface.co/blog/Doctor-Shotgun/llamacpp-moe-offload-guide),
[flags reference](https://openclawdc.com/blog/llama-cpp-moe-offload-flags-explained/), both checked 2026-09-30) —
same active-vs-total logic as §c, per-request instead of per-model.

**Expected speeds:** community reports for gpt-oss-120b with expert offload:
**~35 tok/s on an NVIDIA DGX Spark**; on an RTX Pro 6000 (96 GB, bigger than
the 32 GB GPU), **134 tok/s at 12k context, down to 48 tok/s at max context**
([hardware-corner](https://www.hardware-corner.net/guides/rtx-pro-6000-gpt-oss-120b-performance/), checked 2026-09-30).
Same source: DDR5 at 2000 MT/s instead of 6000 MT/s caused a **3× slowdown**
for this technique — offloaded experts live in RAM, so RAM speed dominates.
Confirming `local-node`'s DDR5-6400 actually runs at rated speed (not a
JEDEC fallback) is a prerequisite for trusting local numbers; not checked
here (blocked by §g anyway).

**Does it beat a smaller all-GPU model here?** Judgment call: for
short-to-medium, parallelism-hungry PRD/council workloads, a smaller
all-GPU model (`qwen3.5:35b`, `qwen3.8:27b` — both fit the 32 GB GPU or B200
without offload and support real `NUM_PARALLEL` fan-out) is very likely the
better *concurrency* choice. CPU+GPU offload for these two big MoE models
fits better as a **quality-tier single-stream option** — one hard PRD task
or one high-stakes persona at a time — not as additional concurrent slots.

---

## (e) VRAM budget worksheets

**Formula** (per brief, matches a public walkthrough —
[KV cache deep dive](https://sebastianraschka.com/llm-architecture-gallery/kv-cache-calculations/), checked 2026-09-30):

```
KV bytes/token = 2 × layers × kv_heads × head_dim × bytes_per_element
VRAM_kv(node)  = KV bytes/token × num_ctx × parallel_slots
```
`bytes_per_element`: 2 (`f16`), ~1 (`q8_0`), ~0.5 (`q4_0`).

**Live `/api/show` architecture numbers** (`http://datawatch:11434`, 2026-09-30):

| Model | layers | kv_heads | head_dim | KV/token f16 | KV/token q8_0 | Weights (disk) |
|---|---|---|---|---|---|---|
| `qwen3:8b` | 36 | 8 | 128 | 144.0 KiB | 72.0 KiB | 5.2 GB |
| `qwen3.8:27b` | 65 | 4 | 256 | 260.0 KiB | 130.0 KiB | 17.7 GB |
| `gemma3:12b` | 48 | 8 | 256 | 384.0 KiB | 192.0 KiB | 8.1 GB |
| `gpt-oss:120b` | 36 | 8 | 64 | 72.0 KiB | 36.0 KiB | 65.4 GB |
| `qwen3-coder-next` | 48 | N/A (hybrid) | — | formula n/a | — | 51.7 GB |

**Caveats, stated explicitly:** `qwen3.8:27b`/`qwen3-coder-next` both report
`full_attention_interval: 4` (hybrid linear attention on most layers) —
their rows are **upper bounds**, `qwen3-coder-next` more so since the
formula doesn't apply to its `head_count_kv: None` layers at all.
`gpt-oss:120b`/`gemma3:12b` use sliding-window attention on some layers, so
those rows are upper bounds too. MoE does **not** shrink KV cache — it
depends on heads/layers, not active-param count.

**Plan 1 — B200 128 GB (`datawatch-openai`):** `qwen3.8:27b` ×4 slots @ 32k
ctx q8_0 + `gemma3:12b` + `nomic-embed-text`:

| Component | Calc | GB |
|---|---|---|
| `qwen3.8:27b` weights | disk | 17.7 |
| `qwen3.8:27b` KV, 4×32k, q8_0 (upper bound) | 130 KiB×32768×4÷1024² | 16.3 |
| `gemma3:12b` weights + KV (1×8k, q8_0) | disk + 192 KiB×8192÷1024² | 9.6 |
| `nomic-embed-text` weights | disk | 0.3 |
| Overhead (~1.3 GB × 3 models) | | 3.9 |
| **Total** | | **~47.8 / 128 GB** |

~80 GB headroom — enough to also load `gpt-oss:120b` (65.4 GB) for the §d
"hard problem" tier, though that gets tight and would need real measurement.

**Plan 2 — 32 GB GPU 32 GB, matches brief's example:** 8× `qwen3:8b` @ 16k
ctx q8_0:

| Component | Calc | GB |
|---|---|---|
| Weights | disk | 5.2 |
| KV, 8×16k, q8_0 | 72 KiB×16384×8÷1024² | 9.0 |
| Overhead | | 1.5 |
| **Total** | | **~15.7 / 32 GB** |

~16 GB headroom — room for `moondream` (1.7 GB) or `nomic-embed-text`
(0.3 GB) concurrently, or to double context to 32k (~24.9 GB total, still fits).

**Plan 3 — 32 GB GPU, mixed vision+chat tenancy** (reflects actual local
tenancy per `ollama_stats`): 6× `qwen3:8b` @ 16k q8_0 + `moondream` +
`nomic-embed-text`:

| Component | Calc | GB |
|---|---|---|
| `qwen3:8b` weights + KV (6×16k q8_0) | 5.2 + 72 KiB×16384×6÷1024² | 11.95 |
| `moondream` + `nomic-embed-text` weights | disk | 2.0 |
| Overhead (3 models) | | 3.9 |
| **Total** | | **~17.9 / 32 GB** |

Headroom to bump back to 8 slots (Plan 2) while keeping vision/embeddings
warm — Plans 2 and 3 aren't mutually exclusive.

**Plan 4 — B200, `gpt-oss:120b` single-stream (no CPU split, fits at
128 GB):** 2 slots @ 16k q8_0:

| Component | Calc | GB |
|---|---|---|
| Weights | disk | 65.4 |
| KV, 2×16k, q8_0 (upper bound) | 36 KiB×16384×2÷1024² | 1.1 |
| Overhead | | 1.5 |
| **Total** | | **~68.0 / 128 GB** |

Cheap in KV terms (narrow `head_dim`, few layers) — the constraint here is
almost entirely the 65.4 GB of weights, so raising `num_ctx` is nearly free.

---

## (f) Multi-node balancing

Live registry state (`compute_node_list`/`health`/`llm_list`, 2026-09-30):

| Node | Kind | Priority | Declared capacity |
|---|---|---|---|
| `datawatch` | `ollama` | 90 | none declared (§a gap) |
| `datawatch-openai` | `openai-compat` | 90 | 1× B200, 128 GB, `max_concurrent_models: 3` |
| `local-node` | `ollama` | 70 | 1× 32 GB GPU, 32 GB, `max_concurrent_models: 10` |
| `test-node` | `ollama` | 50 | stale duplicate of `datawatch`'s address |

Capacity ledger (live): pool `host` limit 9 (external 3), pool
`node:datawatch` limit **1**. That single-lease limit bottlenecks any
attempt to run parallel council personas or PRD workers on `datawatch`
regardless of §e's VRAM headroom — raising it (or routing more to
`local-node`, priority 70 but not capacity-constrained the same way) is a
prerequisite for real 8+ concurrency, not optional polish.

**Existing routing hooks** (`routing` field, orthogonal to `kind`, confirmed
via live `docs_search`/`docs_read` against the compute-nodes doc):
`direct` (default, all four current nodes use it); `docker-network` (daemon
manages container lifecycle via Docker CLI, `auto_start`/`auto_pull`) — the
natural hook for spinning up a vLLM/SGLang container against the B200 on
demand per §b; `datawatch-proxy` (forward through a federated peer's
`/api/proxy/llm/<name>`) — relevant if a future CPU-only endpoint (§c) lives
behind another datawatch instance rather than being registered directly.

**Council balancing sketch:** 12 personas, `council.max_parallel: 0` →
default 2/round, `council.llm_ref` empty → default backend (all confirmed
live). Round-robining across `datawatch` (capacity-limited to 1 lease today)
and `local-node` (real headroom per Plans 2/3) would bottleneck almost
entirely on the `node:datawatch` pool limit, not GPU memory — raising
`council.max_parallel` without first raising that pool limit just produces
queued/rejected leases. A CPU-only endpoint (`llama-server` on
`local-node`'s spare threads, §c's small-model personas) registered as a
fourth compute node with its own pool would let low-stakes personas run
without competing for the 32 GB GPU's GPU slots at all.

**Cost caveat:** `cost_summary` reports 2,243 sessions, `$0`/zero tokens
across every backend — token accounting doesn't work today, so none of the
above can be tuned by cost; capacity/VRAM headroom (this doc) are the only
real signals available until that's fixed.

---

## (g) Prerequisite: local `nvidia-smi` driver/library mismatch

`nvidia-smi` on `local-node` reports **"Driver/library version mismatch"**
(NVML 595.91) — the kernel module and userspace driver library disagree on
version, typically from a driver upgrade on disk while the old module stayed
loaded ([troubleshooting guide](https://support.exxactcorp.com/hc/en-us/articles/32810166604183-GPU-Troubleshooting-Guide-Resolving-Driver-Library-Version-Mismatch-Errors), checked 2026-09-30).

**This blocks any local GPU benchmarking** implied by §c/§d/§e's "verify
locally" caveats — until fixed, the only real 32 GB GPU facts available are the
registry's *declared* capacity, not a live GPU query. Standard remediation,
described in words per scope (not executed here):

1. Identify anything holding the GPU open (inference server, display
   session, container) — the kernel module can't unload while `/dev/nvidia*`
   is held open.
2. Either **reboot** (simplest, guarantees matching versions), or **unload
   and reload the NVIDIA kernel modules** in dependency order
   (`nvidia_uvm`, `nvidia_drm`, `nvidia_modeset`, then `nvidia`) — `nvidia-smi`
   itself triggers the reload afterward. This interrupts any active display
   session, so best done from a text console on a headless box like this one.
3. Re-run `nvidia-smi` to confirm driver/NVML versions agree before trusting
   any local VRAM/utilization number this doc's estimates stand in for.

---

## Summary: getting to 8+ balanced concurrent calls

1. Raise the `node:datawatch` capacity pool limit (currently 1) — the single
   biggest blocker to real concurrency on the highest-priority node.
2. Start with Plan 2/3 (§e) on `local-node`: 6–8 `qwen3:8b` slots @ 16k
   ctx, q8_0 KV, comfortably fits the 32 GB GPU alongside vision/embedding tenants.
3. On the B200, prefer several mid-size-model slots (Plan 1) over one large
   MoE model (Plan 4) for throughput; reserve `gpt-oss:120b`/`qwen3-coder-next`
   CPU-offload (§d) for single-stream "hard problem" tasks, not concurrency.
4. Register the Threadripper's spare cycles as their own compute node
   (`direct` or `docker-network` routing) for embeddings, Whisper, guardrail
   classification, and small council personas — real idle capacity (93 GiB
   free RAM, 48 threads) competing with nothing on the GPU nodes today.
5. Fix the local `nvidia-smi` mismatch (§g) before trusting any
   locally-measured number over the declared-capacity estimates here.
6. Treat `qwen3.8:27b`/`qwen3-coder-next`'s hybrid-attention KV numbers in
   §e as upper bounds — their real parallel-slot ceiling is likely higher
   than this conservative math suggests.
