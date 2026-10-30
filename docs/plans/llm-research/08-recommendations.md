# 08 — Recommendations

**Read this first if you're reading only one document.** Re-verified live
2026-09-30 (retry pass, this run) via `get_version`, `compute_node_list`,
`llm_list`, `cost_summary`, `cost_rates`, `capacity_status`,
`council_config_get`, `autonomous_config_get`, `ollama_stats`,
`eval_list_suites`. No material drift since [06](06-benchmark-plan.md)'s
same-day check: daemon still `8.36.10` (`8.36.11` shown as available);
session counter `2,298` → `2,302`, still `$0`/`0 tokens` everywhere;
`eval_list_suites` still `[]`. Two new data points: `autonomous_config_get`
now reports `max_parallel_tasks: 3` (05 had caught it live-drifted to `0` —
it has since drifted back, a second confirmation this field is unstable,
not a one-off), and a `verification_backends: ["ollama-local-node",
"ollama-datawatch"]` array field is present alongside the older singular
`verification_backend` (07 flagged this field's appearance; still unread in
this pass — noted, not investigated).

**Correction from the prior pass of this document.** An earlier version of
this file was written before [05 — Serving and Hardware](05-serving-and-hardware.md)
and [06 — Benchmark Plan](06-benchmark-plan.md) existed (both were
commissioned and written after this document's first draft — see the
[README](../README.md)'s numbering note), and said so explicitly in place
of citing them. Both files exist now. §3 below is rewritten to draw its
layout directly from 05(e)'s VRAM worksheets as the brief asked, and §5's
phase success criteria now cite 06 §6's quality/throughput decision rule
by name instead of restating an ad hoc equivalent.

## 1. One-page summary

- **The two highest-stakes call sites are already correctly pinned and
  should stay put**: interactive sessions on `claude-code`, and PRD
  planning/coding-worker execution on `qwen3.8:27b`@`datawatch`. Every
  smaller/faster candidate surveyed in [03](03-text-model-candidates.md) is
  a *quality-shape* argument, not a measured win — there is still no
  cost/duration baseline for the incumbent to beat.
- **The real opportunity is the fast/balanced tier**, which today all
  funnels through one shared fallback: `qwen3:1.7b`@`local-node`, the
  smallest general-purpose model in the fleet, deterministically backing
  the verifier, guardrail, Council personas, *and* Council synthesis
  ([01(c)](01-current-state.md#c-llm-registry-and-drift) items 4-6). One
  live exception: the verifier has since drifted onto
  `qwen3.8:27b`@`datawatch` (confirmed again this session), now contending
  with planner/worker for `node:datawatch`'s single session slot — the
  sharpest open fork in this document (§7).
- **The hardware for the "8+ parallel calls" goal already exists and is
  mostly idle.** `local-node`'s 32 GB GPU runs one 0.57 GB embedding
  model most of the time out of 32 GB declared
  ([02, profile 9](02-workload-profiles.md#9-memory-embedding-nomic-embed-text)).
  The binding constraints are config, not silicon: `node:datawatch`'s
  `max_concurrent_sessions: 1` and `council.max_parallel: 0` (→ default 2,
  or possibly serial — the two authoritative docs disagree, see §6).
- **Cost/eval observability is not a minor gap, it's absent.**
  `cost_summary` shows 2,302 sessions at `$0`/`0 tokens` across all seven
  backends; `eval_list_suites` is `[]`. [06](06-benchmark-plan.md) turns
  this into a concrete decision rule (§5 below) but cannot apply it until
  real numbers exist — no routing decision below can be benchmarked against
  real cost or quality data until
  [07 Part B](07-datawatch-improvements.md#part-b--usage-cost-and-performance-tracking)'s
  B1 ships.
- **Two real environment defects need fixing before any routing change**:
  the local NVML/driver mismatch blocking GPU telemetry on `local-node`
  ([01(a)](01-current-state.md#a-hardware-per-compute-node)), and `Ollama`
  version skew (0.33.2 on `datawatch` vs. 0.24.0 on `local-node`) that
  could silently affect quantization-format support if models move between
  hosts.

## 2. Recommended model per workload

Tiers and incumbents from [02](02-workload-profiles.md)'s routing table;
candidates from [03](03-text-model-candidates.md); node-placement and
effort argument from [07](07-datawatch-improvements.md) Part A.

| Workload | Incumbent today | Recommended primary | Fast/parallel alternative | Node | Expected gain | Evidence |
|---|---|---|---|---|---|---|
| PRD planning | `qwen3.8:27b` via opencode | **Keep qwen3.8:27b** — no swap yet | Qwen3-30B-A3B (MoE) as a *benchmarked challenger only* | `datawatch` | None claimed — measure first | [02 profile 1](02-workload-profiles.md#1-prd-decomposition--planning-decomposefn), [03 Frontier tier](03-text-model-candidates.md#frontieragentic-tier-prd-decomposition-coding-worker-sessions) |
| Coding worker sessions | `qwen3.8:27b` via opencode | **Keep qwen3.8:27b** | Same MoE challenger as above | `datawatch` | None claimed — measure first | [02 profile 2](02-workload-profiles.md#2-coding-worker-sessions-opencode--claude-code), 03 Frontier tier |
| Verifier | **Live-drifted**: `qwen3.8:27b`@`ollama-datawatch` (was `qwen3:1.7b` per 01; changed since, confirmed again this session) | Qwen3-4B-Instruct-2507 (fast-small) | Phi-4-mini (MIT, no license review) | `local-node` | Frees `node:datawatch`'s 1-session slot from verifier contention — but see §7 fork 5 before applying | [07 live-drift note](07-datawatch-improvements.md), [03 Fast-small tier](03-text-model-candidates.md#fast-small-tier-verifier-guardrail-memory-embedding-fact-extractionsummarization) |
| Guardrail | `qwen3:1.7b` (fallback; subsystem disabled) | Qwen3-4B-Instruct-2507 | Phi-4-mini | `local-node` | Ready-to-run once `orchestrator.enabled: true` | [02 profile 4](02-workload-profiles.md#4-guardrail-checks-autonomousguardrail), 03 Fast-small tier |
| Council personas (×12) | `qwen3:1.7b` (fallback; `council.llm_ref` empty) | Qwen3-30B-A3B (MoE) | Gemma3:12b — already installed and loaded, zero-effort quick win | `local-node` | Better adversarial-persona quality (`hacker`/`app-hacker`/`security-skeptic` currently on the fleet's smallest model) without linear compute cost | [02 profile 5](02-workload-profiles.md#5-council-persona-proposals-12-personas), [03 Balanced tier](03-text-model-candidates.md#balanced-tier-council-personas-vision-voicestt-once-wired-up) |
| Council synthesis | Same `qwen3:1.7b` fallback | `qwen3.5:35b` — already installed, 23.9 GB | None recommended — do not downsize this call | `datawatch` (already has the model) | Fair reconciliation of 12 viewpoints instead of the smallest model in the fleet doing it | [02 profile 6](02-workload-profiles.md#6-council-synthesis-consensus--dissent), [03 Strong tier](03-text-model-candidates.md#strong-tier-council-synthesis-interactive-chat-sessions) |
| Vision | `moondream:latest` | **Keep** — already correctly pinned | — | `local-node` | None needed | [02 profile 7](02-workload-profiles.md#7-vision-description-image--short-text) |
| Voice/STT | Whisper `medium`, local venv | **Keep** — live and wired, not a gap | Consider CPU-node isolation (07 Proposal 3) | `local-node` (CPU) | Separates STT load from GPU-resident Ollama traffic | [02 profile 8](02-workload-profiles.md#8-voice--speech-to-text-signal-telegram-slack-voice-notes), [07 Proposal 3](07-datawatch-improvements.md#proposal-3--cpu-compute-node-profile) |
| Memory embedding | `nomic-embed-text` | **Keep** — already correctly pinned | Consider CPU-node move (07 Proposal 3) — GPU is idle for this anyway | `local-node` | Frees the one bit of 32 GB GPU VRAM this workload occupies for the balanced-tier models above | [02 profile 9](02-workload-profiles.md#9-memory-embedding-nomic-embed-text), 07 Proposal 3 |
| Fact extraction / summarization | `qwen3:1.7b` via `ollama-local-node` | **Keep** — already the fleet's smallest model, a validation case not a downsizing target | — | `local-node` | None needed | [02 profile 10](02-workload-profiles.md#10-memory-fact-extraction--session-summarization) |
| Interactive sessions | `claude-code` (opus default) | **Keep claude-code** | — | External API | Correctly pinned — human-in-the-loop latency, no local candidate competes | [02 profile 11](02-workload-profiles.md#11-interactive-chat-sessions-claude-code--opencode--ollama-chat), [03 Strong tier](03-text-model-candidates.md#strong-tier-council-synthesis-interactive-chat-sessions) |

**No candidate in this table has been benchmarked against real fleet
traffic.** [03](03-text-model-candidates.md) itself flags roughly 20 failed
`web_search` queries and marks most per-model specs "training knowledge,
unverified live" — one confirmed finding (the GLM line has already moved
two major versions past what was surveyed) is reason enough to re-check
every row before pulling a model into production.

## 3. Target layout for 8+ parallel calls

Drawn directly from [05(e)](05-serving-and-hardware.md#e-vram-budget-worksheets)'s
worked VRAM-budget plans (the "docs/05 worksheets" this document's brief
asked for), mapped onto this document's own role assignments from §2, plus
[05(f)](05-serving-and-hardware.md#f-multi-node-balancing)'s capacity-pool
finding.

**`local-node` (32 GB GPU, 32 GB VRAM, `max_concurrent_models: 10`,
Threadripper 9960X 24c/48t, 122 GB RAM, 93 GiB free):** 05's Plan 3 (mixed
vision+chat tenancy, ~17.9/32 GB) is the closest existing worksheet to this
tier's actual mix once the fast/balanced-tier roles replace its generic
`qwen3:8b` slots:

| Model | Role | Resident (weights+KV) | Target parallel slots | Context |
|---|---|---|---|---|
| `nomic-embed-text` | embed | 0.3 GB | High (many tiny concurrent calls) | N/A |
| Qwen3-4B-Instruct-2507 | verifier | ~2.5 GB (05(e) `qwen3:8b`-class row, scaled down) | 2-3 (`OLLAMA_NUM_PARALLEL`) | 4-8K (diff capped at 8 KB) |
| Phi-4-mini | guardrail | ~2.5 GB | 1-2 | 4-8K |
| Qwen3-30B-A3B (MoE) | council persona | ~19 GB resident, ~3B active/token (05(d)'s active-vs-total logic) | 4-6 (raise `council.max_parallel`) | 8-16K |
| `moondream:latest` | vision | 1.7 GB | 1-2, call-by-call | Small |
| Whisper `medium` (CPU, not Ollama) | speech | N/A — separate pool, per 05(c) | Independent of the above | N/A |

Rough VRAM total with all resident: ~26 GB of 32 GB declared — in the same
range as 05's Plan 2 (~15.7 GB for 8×`qwen3:8b`) and Plan 3 (~17.9 GB),
confirming there's headroom for one more small model or a larger `num_ctx`
on the MoE persona model. Embed + verifier + guardrail + persona alone
already reach **8-12 nominal concurrent slots** on one card, without
touching `datawatch` at all — matching 05's own Plan 2/3 conclusion that
the 32 GB GPU is comfortably under budget for this shape.

**`datawatch` (undeclared hardware — 05(a) flags it as likely the same
physical box as the 128 GB B200 declared for `datawatch-openai`, not
independently confirmed):** 05's Plan 1 (~47.8/128 GB: `qwen3.8:27b`×4
slots@32k q8_0 + `gemma3:12b` + `nomic-embed-text`) shows real headroom
exists on the B200 side; this fleet's actual placement is narrower today:

| Model | Role | VRAM | Slots | Context |
|---|---|---|---|---|
| `qwen3.8:27b` | planner + worker | 15.5 GB | **1** — `node:datawatch`'s `max_concurrent_sessions: 1` is the binding constraint today (05(f), confirmed live again this pass: pool `node:datawatch` limit still `1`) | Large, sustained |
| `qwen3.5:35b` | council synthesis | 23.9 GB | 1, sequential per round | Large (holds all 12 persona responses) |

Combined footprint (~39 GB) is well under 05 Plan 1's demonstrated ~47.8 GB
budget for four times the `qwen3.8:27b` slots — the ceiling here is the
`max_concurrent_sessions: 1` session gate, **not** VRAM, exactly as 05(f)
concludes ("raising it... is a prerequisite for real 8+ concurrency, not
optional polish"), though 05 doesn't weigh the VRAM-pressure/B200-sharing
trade-off §7 fork 6 raises for actually doing so. Moving the verifier off
this node (§7 fork 5) is a narrower alternative that raises `datawatch`'s
usable concurrency for planner/worker traffic without touching the pool
limit at all — the two forks are independent levers, not sequenced steps.

**Net (05's own finding, §3 restated for this fleet's role mix): 8+
parallel calls is reachable today primarily by (a) resolving §7 fork 6
(raising the `node:datawatch` capacity pool limit — 05(f)'s "single biggest
blocker," but a real trade-off, not a free action) and/or fork 5 (verifier
relocation), (b) raising `council.max_parallel` toward its natural 12-wide
shape, and (c) fixing the `local-node` `nvidia-smi` mismatch (05(g))
before trusting any locally-measured number over these declared-capacity
estimates. None of this requires new hardware.**

## 4. Operator-side configuration changes

Described in words first, illustrative snippets after. **None of this was
applied — read-only tools only were used this session.**

- **If §7 fork 6 is decided in favor of raising it: raise the
  `node:datawatch` capacity pool limit** from `1`
  ([05(f)](05-serving-and-hardware.md#f-multi-node-balancing)'s "single
  biggest blocker to real concurrency" finding, reconfirmed live this pass
  via `capacity_status`) — the prerequisite for §3's Plan-1-style
  `qwen3.8:27b` multi-slot fan-out on `datawatch`. This document does not
  decide fork 6 itself (trade-off: more simultaneous VRAM pressure on a GPU
  whose sharing with `datawatch-openai` isn't fully confirmed, §6) — it only
  shows what applying it would look like.
- **Add a `roles:` block** ([07 Proposal 1](07-datawatch-improvements.md#proposal-1--workload-role-model-tiers-role--llm-mapping))
  mapping the nine workload roles to registry entries, with an explicit
  warning when two roles would share a `max_concurrent_sessions`-constrained
  node.
- **Raise `council.max_parallel`** from `0` toward the persona count (exact
  target is §7 fork 7 — this document does not pick a number).
- **Set `council.llm_ref`** to whichever registry entry backs the new
  balanced-tier model, once §7 fork 4 is decided.
- **Delete `test-node`** — stale duplicate of `datawatch`'s address, unused
  since 2026-09-17 ([01(c)](01-current-state.md#drift--registry-vs-reality-all-confirmed-live-this-pass) item 2).
- **Run `llm_refresh_models` on `ollama-local-node`** — its model list
  still names five models not installed on that host
  ([01(c)](01-current-state.md#drift--registry-vs-reality-all-confirmed-live-this-pass) item 1).
- **Add a CPU-only compute node** for embed/speech
  ([07 Proposal 3](07-datawatch-improvements.md#proposal-3--cpu-compute-node-profile)).
- **Add local-compute cost-model fields** to both GPU nodes so `cost_rates`
  stops reporting flat `$0` for local inference
  ([07 Proposal B2](07-datawatch-improvements.md#proposal-b2--local-compute-cost-model)).

```yaml
# illustrative — not applied
roles:
  planner:          { llm_ref: ollama-datawatch, model: qwen3.8:27b }
  worker:           { llm_ref: ollama-datawatch, model: qwen3.8:27b }
  verifier:         { llm_ref: ollama-local-node, model: qwen3-4b-instruct-2507 }
  guardrail:        { llm_ref: ollama-local-node, model: phi-4-mini }
  council-persona:  { llm_ref: ollama-local-node, model: qwen3-30b-a3b }
  council-synthesis: { llm_ref: ollama-datawatch, model: qwen3.5:35b }
  vision:           { llm_ref: ollama-local-node, model: moondream:latest }
  embed:            { llm_ref: ollama-local-node, model: nomic-embed-text }
  speech:           { llm_ref: whisper-local, model: medium }

council:
  llm_ref: ollama-local-node
  max_parallel: 6   # up from 0; exact target is an open operator decision, see 08 section 7
```

```yaml
# illustrative — not applied. Only if operator fork 6 (section 7) is
# decided in favor of raising this limit.
capacity_pools:
  node:datawatch:
    limit: 4   # up from 1 — see 05(f); exact target should follow phase-1
               # benchmark data, not this illustrative number
```

```yaml
# illustrative — not applied
compute_nodes:
  - name: local-node-cpu
    kind: ollama
    address: http://local-node:11435   # separate CPU-only Ollama instance, port illustrative
    scheduling_priority: 40
    role_affinity: [embed, speech]
    hardware: { gpu_count: 0 }

  - name: local-node
    declared_capacity:
      gpu_watts: 575          # NVIDIA 32 GB GPU spec, nvidia.com, checked 2026-09-30
      # capex_usd / expected_lifetime_hours / avg_utilization_pct: operator-supplied, not invented
```

## 5. Phased rollout

Phase 1/2 success criteria below apply
[06 §6](06-benchmark-plan.md#6-decision-rule)'s decision rule by name: a
candidate **replaces** an incumbent only when it scores ≥90% of the
incumbent's quality on that tier's mapped eval suite (parity, no tolerance,
for the PRD/verifier/guardrail suites 06 marks catastrophic-on-failure) *and*
≥2× the incumbent's decode tok/s at the tier's real target concurrency,
measured on the same pass. A candidate meeting only one axis is a flagged
fallback, not a swap (06 §6). Phases 0/3/4 draw criteria from this
document's own findings, since 06's rule is specific to model-swap
decisions (Phases 1-2).

**Phase 0 — Fix drift and driver mismatch.** Delete `test-node`; refresh
`ollama-local-node`'s model list; resolve the verifier-placement fork
(§7 fork 5) explicitly, so it stops being silent drift; reload/reinstall
the NVML userspace library on `local-node`.
*Success:* `compute_node_list` shows no stale nodes; `ollama_stats` on
`local-node` matches the registry exactly; `nvidia-smi` runs clean; the
verifier's backend is a deliberate choice, not whatever it drifted to.

**Phase 1 — Pull and benchmark candidates on the 32 GB GPU.** Follow
[06 §7](06-benchmark-plan.md#7-run-order--cheapest-first)'s cheapest-first
order: fix the measurement environment first (confirm `OLLAMA_NUM_PARALLEL`,
find a low-traffic window — `datawatch` contention was directly observed in
[06 §4](06-benchmark-plan.md#4-observed-timing--seed-data-this-session-2026-09-30));
run the mechanical-grader suites (1, 2, 3, 5) against already-installed
models before pulling anything new; then pull Qwen3-4B-Instruct-2507 first
(cheapest per [03](03-text-model-candidates.md)) against Suites 2/3
(verifier/guardrail), then Phi-4-mini and Qwen3-30B-A3B, recording
tokens/sec, TTFT, and quality score per [06 §3](06-benchmark-plan.md#3-performance-metrics-to-capture-separate-from-quality-suites)'s
metric definitions.
*Success:* every candidate/tier pair has a filled row in
[06 §5](06-benchmark-plan.md#5-results-template)'s results template — a
real, non-blank quality *and* throughput number, not an estimate — closing
[02](02-workload-profiles.md)'s repeatedly-flagged "highest-value gap."

**Phase 2 — Move verifier/guardrail/council personas to the fast/balanced
tier.** Apply the `roles:` block (once built) or the individual
`*_backend`/`llm_ref` keys directly, but **only** for candidates that
passed [06 §6](06-benchmark-plan.md#6-decision-rule)'s decision rule in
Phase 1; raise `council.max_parallel`, and raise the `node:datawatch` pool
limit if §7 fork 6 has been decided in favor of it by this phase.
*Success:* verifier and guardrail run on whichever candidate met both the
≥90%-quality and ≥2×-throughput bars in Phase 1, not the ad hoc fallback
and not a candidate that passed only one axis; `capacity_status` shows
Council rounds actually reaching the configured `max_parallel`, whose real
semantics are disputed (§6) and should be confirmed first.

**Phase 3 — CPU node for embeddings/speech.** Stand up a CPU-only compute
node on the Threadripper's otherwise-idle 93 GiB free RAM / 48 threads;
migrate `embed` and `speech` roles to it via `role_affinity`.
[05(c)](05-serving-and-hardware.md#c-cpu-inference-on-the-threadripper-9960x)'s
bandwidth-bound estimates (~27 tok/s dense-8B class, ~54 tok/s dense-4B,
not independently benchmarked on this exact CPU) set rough expectations —
confirm against real Whisper/embed throughput once live.
*Success:* embedding/Whisper traffic no longer shares a resource pool with
any GPU-resident model on `local-node`; the freed GPU headroom is fully
available to the balanced tier.

**Phase 4 — Datawatch tracking and routing features from docs/07.** Ship
[07 Part B](07-datawatch-improvements.md#part-b--usage-cost-and-performance-tracking)'s
B1 (usage records) and B4 (starter alert rules), then Part A's Proposal 2
(VRAM-aware capacity ledger) and Proposal 4 (node selection policy).
*Success:* `cost_summary` reports non-zero tokens for Ollama/OpenAI-compat
backends; at least one alert rule has fired in a test; `capacity_status`
reports computed-effective concurrency per node, not just a flat limit.

## 6. Risks and open questions

- **Every non-incumbent model recommendation is unbenchmarked.**
  [03](03-text-model-candidates.md) couldn't get `web_search` to resolve a
  single model-card page; the one family confirmed live (GLM) had already
  moved two major versions past what was assumed current. Treat §2's model
  choices as a Phase 1 shortlist, not a final answer.
- **`council.max_parallel`'s runtime behavior is disputed**: the docs
  corpus says `0` defaults to "2 concurrent," but `council_config_set`'s own
  parameter description says `">=0; 0=serial"` ([07](07-datawatch-improvements.md)).
  Confirm before Phase 2 — it changes whether raising the number is tuning
  or turning on parallelism for the first time.
- **MoE serving under Ollama's single-model-slot-per-node behavior is
  unconfirmed** — the active-parameter argument for Qwen3-30B-A3B
  ([03](03-text-model-candidates.md#moe-vs-dense-for-this-fleets-goal)) was
  never tested against real concurrent requests this session.
- **Terminal/subprocess cost tracking (83% of sessions) has an open
  question with no owner**: whether claude-code/opencode expose structured
  token usage at all. If not, [B1](07-datawatch-improvements.md#proposal-b1--per-call-usage-records)'s
  terminal half needs new proxy plumbing, not just a parser.
- **The B200's real per-GPU power draw was never confirmed** — only a
  system-level 8-GPU figure from NVIDIA's DGX B200 page
  ([07 Proposal B2](07-datawatch-improvements.md#proposal-b2--local-compute-cost-model)).
  Any B200 cost-per-token figure here is a bounded range, not a fact.
- **`datawatch` and `datawatch-openai` may be the same physical box** —
  would mean the B200 is shared, not dedicated; not confirmed from this
  project's read-only scope ([01(a)](01-current-state.md#a-hardware-per-compute-node)).
  Matters for §3's layout if true.
- **Ollama version skew** (0.33.2 `datawatch` vs. 0.24.0 `local-node`)
  could silently break quantization compatibility (e.g. MXFP4) if a model
  moves between hosts.

## 7. Decisions for the operator

Presented with trade-offs, not picked silently, per this document's brief.

1. **Ollama vs. vLLM (or another serving engine) on the B200.** Ollama keeps
   one consistent stack fleet-wide, no new ops burden; vLLM's PagedAttention
   continuous batching could materially improve concurrent throughput for
   the frontier tier at concurrency 8-16 — one published benchmark shows
   ~2.8× per-request efficiency gain at concurrency 32 vs. 1
   ([05(b)](05-serving-and-hardware.md#b-alternatives-to-ollama)) — at the
   cost of running two serving engines. `datawatch-openai`'s existing
   `openai-compat` node ([05(f)](05-serving-and-hardware.md#f-multi-node-balancing))
   means this swap doesn't require touching any LLM registry call site.
2. **Whether to add a dedicated voice pipeline change at all.** Whisper
   `medium` already works and is live
   ([02 profile 8](02-workload-profiles.md#8-voice--speech-to-text-signal-telegram-slack-voice-notes)).
   The only proposed change (Phase 3, CPU-node isolation) is worth doing
   only if GPU contention from Council/verifier traffic is actually observed.
3. **Whether to change the memory embedder given re-indexing cost.**
   `nomic-embed-text` is small, GPU-idle-friendly, and works
   ([02 profile 9](02-workload-profiles.md#9-memory-embedding-nomic-embed-text)).
   A stronger embedder could improve recall quality but requires
   re-embedding the entire memory store — that cost wasn't estimated this
   session and should be sized before deciding.
4. **Which Council personas may use a small model.** All 12 share one
   balanced-tier model (simplest, matches today's design) vs. the three
   adversarial personas (`hacker`, `app-hacker`, `security-skeptic`) getting
   a stronger model, since [01(c)](01-current-state.md#drift--registry-vs-reality-all-confirmed-live-this-pass)
   item 4 flagged them on the fleet's smallest model today.
5. **Verifier placement — the sharpest live fork found this session.**
   Currently drifted to `qwen3.8:27b`@`datawatch` (higher quality, but
   contends with planner/worker for the node's single session slot) vs.
   reverting to a fast-small model@`local-node` (relieves contention,
   costs verification quality). Needs Phase 1's benchmark data to answer
   with evidence, not a guess made here.
6. **Whether to raise `node:datawatch`'s `max_concurrent_sessions` above
   1.** An alternative to fork 5 — lets planner/worker/verifier run
   concurrently without moving the verifier, at the cost of more
   simultaneous VRAM pressure on a GPU whose sharing with `datawatch-openai`
   isn't fully confirmed (§6).
7. **Target value for `council.max_parallel`.** Anywhere from 2 (documented
   default) to the full natural width of 12 — depends on Phase 1's real
   throughput numbers for whichever model lands there.
8. **Whether to pursue [07 Proposal 2](07-datawatch-improvements.md#proposal-2--capacity-awareness-for-ollama-parallel-slots-and-vram)
   (VRAM-aware capacity ledger) now or defer it.** Biggest structural
   unlock for "8+ parallel," but also the least confident effort estimate
   (L, admission-ledger internals unread) — early is higher-risk, late means
   reasoning about whole-session slots instead of real headroom.
