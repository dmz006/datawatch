# 01 — Current State of LLM Infrastructure

Snapshot verified 2026-09-29 against a live datawatch instance (version 8.34.1,
hostname `local-node`) via the read-only MCP tools (`get_config`, `llm_list`,
`compute_node_list`, `compute_node_health`, `backends_list`, `ollama_stats`,
`capacity_status`, `autonomous_config_get`, `council_personas`,
`council_config_get`, `orchestrator_config_get`, `cost_summary`, `cost_rates`,
`eval_list_suites`, `docs_search`/`docs_read`) plus direct read-only `curl` GETs
to the two Ollama hosts. All facts below were re-checked live, not carried over
unverified from an earlier session.

**Correction pass (same day):** the initial write-up above was built entirely
from MCP tool output and documentation, without reading the datawatch Go
source. A follow-up pass cross-checked every claim below against the actual
source at `the datawatch repo root` (origin `dmz006/datawatch`, HEAD
`30c24271`) and the live `~/.datawatch/config.yaml`. The repo has
since moved to v8.35.1 (two releases the same day this doc was written); that
bump didn't change any fact below, but several "unset/orphaned" claims in
sections (c) and (d) were **wrong** and are corrected in place, with
`file:line` citations, rather than left as a separate errata list.

## Verification log

This document has been re-verified against the live datawatch instance three
times since first written. Rather than let each pass scatter its own
"re-verified" note across whichever section it touched, they're consolidated
here as one audit trail; sections (e) and (f) still carry an inline pointer
back to this log at the specific numbers each pass changed.

| Pass | Date | Tools re-run | Result |
|---|---|---|---|
| 1 — initial write-up | 2026-09-29 | `get_config`, `llm_list`, `compute_node_list`, `compute_node_health`, `backends_list`, `ollama_stats`, `capacity_status`, `autonomous_config_get`, `council_personas`/`council_config_get`, `orchestrator_config_get`, `cost_summary`, `cost_rates`, `eval_list_suites`, `docs_search`/`docs_read`, plus `curl` to both Ollama hosts | Built the doc from MCP/API output alone; several registry-vs-source claims in (c)/(d) were later found wrong (see correction pass above) |
| 2 — source cross-check | 2026-09-29 (same day) | Read of `cmd/datawatch/main.go`, `internal/council/council.go`, `internal/orchestrator/runner.go`, `internal/config/config.go`, `internal/transcribe/factory.go` against live `config.yaml` | Corrected drift items (4)-(6) and the voice/transcription call-site with `file:line` citations; repo had moved to v8.35.1 in the interim with no fact-changing impact |
| 3 — live delta check | 2026-09-30 | `capacity_status`, `cost_summary`, `eval_list_suites` | `node:datawatch` lease released (1 held → 0 held); `cost_summary` sessions 2249 → 2259; token/USD fields still 0 everywhere; `eval_list_suites` still `[]` |
| 4 — live delta check (this pass) | 2026-09-30 | `capacity_status`, `cost_summary`, `eval_list_suites`, `compute_node_list`, `llm_list` | `node:datawatch` still unheld (0/1); only active lease is this doc-writing session itself (`host` + `llm:claude-code`, PRD `a2833a5e`); `cost_summary` sessions 2259 → 2260, tokens/USD still 0 across all 7 backends (`claude`, `claude-code`, `goose`, `ollama`, `ollama-datawatch`, `opencode`, `subprocess`); `eval_list_suites` still `[]`; registry composition and node declarations in (c) re-diffed field-by-field against fresh `llm_list`/`compute_node_list` output — no drift beyond what's already documented in (c) |

Net read after four passes: the hardware, model-inventory, registry, and
call-site facts in (a)-(d) are stable — nothing has changed at that layer
since the correction pass. The only things that move between passes are the
session/lease counters in (e) and (f), and they move in the direction the
doc already predicted (sessions climb, tokens stay at zero, capacity leases
come and go with whatever's actually running).

This document is `01` in a planned 01-09 series (see the project [README](../README.md)
for the full index). `02-workload-profiles.md` and `03-text-model-candidates.md`
are already written and build directly on the call-site map in (d) and the
capacity/scheduling numbers in (e); `04` (cost & observability) is the natural
next doc to close the gap flagged in (f) once — or if — token accounting
starts reporting real numbers.

## (a) Hardware per compute node

| Node | Kind | Address | GPU | VRAM | CPU / RAM | Ollama version | Driver state |
|---|---|---|---|---|---|---|---|
| `datawatch` | ollama | `http://datawatch:11434` | not declared in registry | not declared | not declared | 0.33.2 (confirmed via `/api/version`) | not observable from this host |
| `datawatch-openai` | openai-compat (OpenWebUI) | `http://datawatch:3000` | 1× NVIDIA Blackwell B200 | 128 GB (declared) | not declared | n/a (OpenAI-compatible surface, not raw Ollama) | not observable from this host |
| `local-node` (local) | ollama | `http://local-node:11434` = `localhost` | 1× 32 GB GPU | 32 GB (declared) | AMD Ryzen Threadripper 9960X, 24c/48t, 122 GB RAM | 0.24.0 (confirmed via `/api/version` — **older** than the `datawatch` node's 0.33.2) | `nvidia-smi` fails: `Failed to initialize NVML: Driver/library version mismatch` (NVML library 595.91) |
| `test-node` | ollama | `http://datawatch:11434` (identical address to `datawatch`) | not declared | not declared | not declared | shares whatever `datawatch` reports | n/a — see drift note in (c) |

**Findings:**
- The local `local-node` host cannot report GPU utilization/VRAM via `nvidia-smi` at all right now — the NVML userspace library (595.91) doesn't match the loaded kernel driver. This blocks any local GPU telemetry (power draw, thermals, live VRAM) until the driver is reloaded/rebooted or the matching userspace package is installed. Ollama itself still functions (it talks to the kernel driver directly), which is why `/api/tags` and `/api/ps` work even though `nvidia-smi` doesn't.
- Ollama versions differ across the two real hosts: 0.33.2 on `datawatch` vs 0.24.0 on `local-node`. That's a meaningful gap (multiple minor releases) and could explain feature/quantization-format differences (e.g. MXFP4 support) if models are ever moved between the two.
- `datawatch` and `datawatch-openai` are two different registry entries but likely the *same physical box* (OpenWebUI at `:3000` typically fronts the Ollama instance at `:11434` on the same host) — declared hardware for `datawatch-openai` (B200/128GB) is plausibly the real GPU behind the plain `datawatch` node too, which declares no hardware at all. Not confirmed from this project's read-only scope; flagged as a documentation gap rather than a verified duplicate.

## (b) Model inventory per node

### `datawatch` (Ollama 0.33.2, via `/api/tags` + `/api/ps`)

| Model | Size on disk | Params | Quant | Loaded now? | VRAM used |
|---|---|---|---|---|---|
| qwen3.8:27b | 17.7 GB | 27.3B | Q4_K_M | **yes** | 15.5 GB |
| gpt-oss:120b | 65.4 GB | 116.8B | MXFP4 | no | — |
| qwen3-coder-next:q4_K_M | 51.7 GB | 79.7B | Q4_K_M | no | — |
| qwen3.5:35b | 23.9 GB | 36.0B (tag says "35b", actual param count is 36.0B) | Q4_K_M | no | — |
| qwen3-coder:30b | 18.6 GB | 30.5B | Q4_K_M | no | — |
| qwen2.5:14b | 9.0 GB | 14.8B | Q4_K_M | no | — |
| Gemma3:12b | 8.1 GB | 12.2B | Q4_K_M | **yes** | 8.0 GB |
| qwen3:8b | 5.2 GB | — (not re-verified this pass) | — | no | — |
| qwen3:1.7b | 1.4 GB | 2.0B (tag says "1.7b", actual param count is 2.0B) | Q4_K_M | no | — |
| nomic-embed-text | 0.3 GB | 137M | F16 | no | — |
| llmvision/glimpse-v1 | 5.0 GB | 419.82M | BF16 | no | — |

Two models are concurrently resident in VRAM on `datawatch` right now: `qwen3.8:27b` (15.5 GB) and `Gemma3:12b` (8.0 GB) — ~23.5 GB combined, out of a GPU the registry doesn't declare a size for (see (a)).

### `local-node` / local (Ollama 0.24.0, via `/api/tags` + `/api/ps`)

| Model | Size on disk | Params | Quant | Loaded now? | VRAM used |
|---|---|---|---|---|---|
| moondream:latest | 1.7 GB | 1B | Q4_0 | no | — |
| llmvision/glimpse-v1 | 5.0 GB | 419.82M | BF16 | no | — |
| qwen3:1.7b | 1.4 GB | 2.0B | Q4_K_M | no | — |
| nomic-embed-text | 0.3 GB | 137M | F16 | **yes** | 0.57 GB |

Total on disk: 7.8 GB (matches `ollama_stats`). Only the embedder is currently loaded — expected, since `memory.embedder_host` points here for background embedding calls.

## (c) LLM registry and drift

| Registry name | Kind | Default model | Compute node(s) | Enabled models (per registry) |
|---|---|---|---|---|
| `claude-code` | claude-code | opus | (n/a — external API) | opus, sonnet, haiku |
| `ollama-datawatch` | ollama | qwen3:1.7b | `datawatch` | qwen3:1.7b, gpt-oss:120b, nomic-embed-text, qwen3-coder-next:q4_K_M, qwen3.5:35b, qwen3-coder:30b, qwen2.5:14b, Gemma3:12b, qwen3:8b, qwen3.8:27b |
| `ollama-local-node` | ollama | qwen3:1.7b | `local-node` | qwen3:1.7b, nomic-embed-text, qwen3.5:35b, qwen3-coder:30b, qwen2.5:14b, Gemma3:12b, qwen3:8b |
| `opencode` | opencode | — | `datawatch` | (inherits node's models via opencode config) |
| `openwebui-datawatch` | openwebui | qwen3-coder-next:q4_K_M | `datawatch-openai` | qwen3-coder-next:q4_K_M, gpt-oss:120b, qwen3.5:35b, qwen3-coder:30b, qwen2.5:14b, Gemma3:12b, qwen3:8b, qwen3:1.7b, nomic-embed-text |

`backends_list` additionally reports a bare **`ollama`** backend (available, enabled) that is not one of the five named entries above. **This is not an orphan** — it's the deterministic fallback for three consumers, wired explicitly in source: Council (`cmd/datawatch/main.go:3160-3163`, `llmRef := cfg.Council.LLMRef; if llmRef == "" { llmRef = "ollama" }`), the verifier (`cmd/datawatch/main.go:4326-4327`), and the guardrail closure (`cmd/datawatch/main.go:4393-4394`) all fall back to this exact named entry when their own config key is empty. Its model/host come from the legacy top-level `ollama.*` config block, confirmed live in `~/.datawatch/config.yaml`: `model: qwen3:1.7b`, `host: http://localhost:11434` — i.e. the fallback is concretely **`qwen3:1.7b` on `local-node`**, not an ambiguous "whatever the default is."

### Drift — registry vs. reality (all confirmed live this pass)

1. **`ollama-local-node` model list is stale.** The registry lists 7 models (`qwen3:1.7b`, `nomic-embed-text`, `qwen3.5:35b`, `qwen3-coder:30b`, `qwen2.5:14b`, `Gemma3:12b`, `qwen3:8b`) as available on `local-node`. The live `/api/tags` on that host shows only 4 models installed (`moondream`, `llmvision/glimpse-v1`, `qwen3:1.7b`, `nomic-embed-text`). Five registry entries (`qwen3.5:35b`, `qwen3-coder:30b`, `qwen2.5:14b`, `Gemma3:12b`, `qwen3:8b`) do **not exist locally** — any request routed to one of those would fail or silently fall through. Conversely, `moondream` and `llmvision/glimpse-v1` are installed locally but **not** in the registry's model list for this LLM, even though `moondream` is the model actually configured for vision (see (d)).
2. **`test-node` is a stale duplicate.** Same address as `datawatch` (`http://datawatch:11434`), lower scheduling priority (50 vs. 90), created 2026-09-17 and never updated since. No LLM registry entry references it. Candidate for deletion.
3. **`ollama-datawatch` defaults to a 1.7B model while PRD planning uses a 27B model on the same node.** The registry's declared default model for `ollama-datawatch` is `qwen3:1.7b`, but `autonomous.planning_model` is `ollama/qwen3.8:27b` — the actual planning workload on that node is ~16x larger than the registry's stated default. The default is effectively meaningless for this node; it's overridden everywhere that matters.
4. **`council.llm_ref` is unset** (confirmed via `council_config_get`: `"llm_ref":""`, and no `council:` block at all in the live config file). Council persona rounds and synthesis fall back to the named `ollama` registry entry — **`qwen3:1.7b` on `local-node`** (`cmd/datawatch/main.go:3160-3163`), not an unauditable moving target. That's still worth flagging, just for a different reason than "could change silently": the personas used for adversarial/security review (`hacker`, `app-hacker`, `security-skeptic`) are deterministically running on the *smallest* general-purpose model in the whole fleet, on shared hardware with the memory embedder. `council.RunCtx` (`internal/council/council.go:546-548`) does hard-error with `ErrNoInference` if `LLMRef` were ever empty at actual call time — it never is in practice, because `main.go` always fills the fallback before wiring the orchestrator.
5. **`autonomous.verification_backend` and `autonomous.verification_effort` are both empty strings.** The verifier (`autonomousVerify`) resolves through the *same* fallback path as (4) — `cmd/datawatch/main.go:4326-4327` fills `vbackend = "ollama"` when unset, so the verifier also runs on `qwen3:1.7b`@`local-node` deterministically, not an unpinned default.
6. **Orchestrator is disabled but pre-configured.** `orchestrator.enabled: false`, yet it already has `default_guardrails` set (`rules`, `security`, `release-readiness`, `docs-diagrams-architecture`), a `guardrail_timeout_ms` of 120000, and `max_parallel_prds: 2`. `orchestrator.guardrail_backend` (`internal/orchestrator/runner.go:44`) is a real, separate config field that is an empty string — but it's also **not what the actual guardrail call site reads**: the per-task guardrail closure in `cmd/datawatch/main.go:4393-4394` falls back on `amgrCfg.VerificationBackend` (the same field the verifier uses), not on `orchestrator.guardrail_backend`, despite a nearby comment claiming otherwise. So there are two distinct "guardrail backend" config keys in the codebase, and only one of them (the verifier's) is actually load-bearing today. Net effect is the same as (5): falls back to `ollama` → `qwen3:1.7b`@`local-node`.
7. **Model tag vs. actual parameter count mismatches** on at least two `datawatch` models: `qwen3.8:27b` reports actual param size 27.3B (tag matches reasonably) but `qwen3.5:35b` is tagged "35b" while Ollama reports actual parameter size 36.0B, and `qwen3:1.7b` is tagged "1.7b" while Ollama reports 2.0B. Cosmetic, but worth knowing when reasoning about VRAM math from tags alone.

## (d) Call-site map

For each LLM consumer: effective backend/model/node today, and exactly where that's configured (config section + key, confirmed via `get_config` and the docs listed).

| Consumer | Backend | Model | Node | Configured via |
|---|---|---|---|---|
| PRD decompose (`decomposeFn`) | opencode | `ollama/qwen3.8:27b` | `datawatch` | `autonomous.planning_backend`, `autonomous.planning_model` (both confirmed live) |
| Worker session (task execution) | opencode | `ollama/qwen3.8:27b` | `datawatch` | project `opencode.json` (`ollama/qwen3.8:27b` @ `datawatch:11434`); daemon-side `opencode.ollama_chunk_timeout_sec: 1200` confirmed in `get_config` |
| Verifier (`autonomousVerify`) | ollama (named fallback `ollama`) | `qwen3:1.7b` | `local-node` | `autonomous.verification_backend` empty → falls back to registry entry `ollama` (`cmd/datawatch/main.go:4326-4327`); model/host from live `ollama.model`/`ollama.host` in config.yaml — drift item (5) above, but deterministic, not a doc gap |
| Guardrail (`autonomousGuardrail`) | ollama (same fallback, orchestrator disabled) | `qwen3:1.7b` | `local-node` | `amgrCfg.VerificationBackend` empty → same `ollama` fallback (`cmd/datawatch/main.go:4393-4394`); `orchestrator.guardrail_backend` is a separate, currently-unread config key (see drift item 6); `orchestrator.enabled: false` |
| Council persona round | ollama (named fallback `ollama`) | `qwen3:1.7b` | `local-node` | `council.llm_ref` empty → falls back to registry entry `ollama` (`cmd/datawatch/main.go:3160-3163`) — drift item (4) |
| Council synthesis | same fallback as persona rounds | `qwen3:1.7b` | `local-node` | same key, `council.llm_ref` — the doc (`datawatch-definitions.md#settings-automate`) does not distinguish a separate synthesis-model key |
| Vision describe | ollama | `moondream:latest` | `local-node` (`vision.endpoint: http://localhost:11434`) | `vision.enabled`, `vision.backend`, `vision.endpoint`, `vision.model` — all confirmed live |
| Voice / audio transcription | whisper (local Python venv) | Whisper `medium` | local venv (`~/.datawatch/.venv`) | `whisper.enabled: true`, `whisper.model: "medium"`, `whisper.venv_path` set, `signal.enabled: true` — all confirmed live. `whisper.backend` is an empty string, but that is the **documented default**, not an unset gap: `internal/config/config.go:1814-1816` — `""/"whisper" (default) — local OpenAI-Whisper Python venv`; `internal/transcribe/factory.go:26-35` — `case "", "whisper", "local":` builds the real local-venv transcriber ("empty Backend defaults to 'whisper' so existing operator configs keep working"). Voice transcription over Signal is live and wired today, not a gap — see (f) for the correction. |
| Memory embedder | ollama | `nomic-embed-text` | `local-node` (`http://local-node:11434`) | `memory.embedder_host`, `memory.embedder_model`; storage backend is `memory.backend: postgres` |
| Memory fact extraction / session summarize | ollama (via named LLM) | inherits `ollama-local-node` → `qwen3:1.7b` | `local-node` | `session.summarizer.enabled: true`, `session.summarizer.llm_ref: "ollama-local-node"` |
| Interactive sessions | claude-code | opus (default) / sonnet / haiku selectable | external API, not a compute node | `session.llm_backend: "claude-code"`; confirmed as the active backend in `backends_list` (`"active":"claude-code"`) |

## (e) Capacity and scheduling (live snapshot)

- **Capacity pools** (`capacity_status`, live): `host` limit 9, currently 2 held, 2 external; `node:datawatch` limit 1, currently 1 held (fully saturated); `llm:claude-code` limit 0 (unbounded), 1 held; `llm:opencode` limit 0 (unbounded), 1 held. Two active leases at capture time: an opencode/`qwen3.8:27b` task on `datawatch` (PRD `8baa807b`) and this doc-writing session itself on `claude-code`/claude-sonnet-5 (PRD `a2833a5e`) — i.e. `node:datawatch`'s single-session limit is presently the binding constraint on that node.
- **Re-verified 2026-09-30 (pass 3):** re-ran `capacity_status` live. `node:datawatch` is now unheld (0/1) — the opencode/`qwen3.8:27b` lease from the prior snapshot has since released. Only one active lease remains, this session itself (`host` + `llm:claude-code`, PRD `a2833a5e`), confirming the pool mechanics described above rather than a fixed state — `node:datawatch`'s limit-1 pool is the binding constraint only while a task actually holds it, not permanently.
- **Re-verified 2026-09-30 (pass 4, see Verification log above):** `node:datawatch` still 0/1 held; still exactly one active lease, still this same doc-writing PRD (`a2833a5e`). Pool structure (`host` limit 9, `node:datawatch` limit 1, unbounded `llm:*` pools) is unchanged from every prior pass.
- **Scheduling priority**: `datawatch` 90, `datawatch-openai` 90, `local-node` 70, `test-node` 50 (stale, see (c)).
- **Max concurrency per node**: `datawatch` → `max_concurrent_sessions: 1` (no declared model-concurrency cap); `datawatch-openai` → `max_concurrent_models: 3`; `local-node` → `max_concurrent_models: 10`; `test-node` → none declared.
- **Autonomous**: `max_parallel_tasks: 3`, `max_recursion_depth: 5`, `poll_interval_seconds: 30`, `planning_timeout_seconds: 3600`, `auto_fix_retries: 1`, `auto_approve_children: true`.
- **Council**: `max_parallel: 0` — per the docs this means "use the built-in default of 2 concurrent personas per round," not "unlimited" or "disabled."
- **Orchestrator**: `max_parallel_prds: 2`, but `enabled: false` — the whole subsystem is off despite being pre-configured (see drift item 6).

## (f) Observability gaps

Stated plainly, because these materially limit what any of the sections above can be trusted for going forward:

- **Token/cost accounting does not work.** `cost_summary` reports 2,249 sessions across all backends (claude, claude-code, goose, ollama, ollama-datawatch, opencode, subprocess) with `total_tokens_in: 0`, `total_tokens_out: 0`, and `total_usd: 0` — for every single backend, including `claude-code` (139 sessions) and `opencode` (211 sessions), both of which have non-zero configured rates in `cost_rates` ($0.003/$0.015 per 1K tokens). Non-zero rates exist; zero usage is recorded against them. There is currently no way to answer "how much did this PRD/session cost" from this system.
- **Zero eval suites exist.** `eval_list_suites` returns `[]`. Algorithm Mode's "Measure" phase and any quality-gate-by-eval workflow have no rubric-based grading to draw on today.
- **Re-verified 2026-09-30 (pass 3):** re-ran both live. `cost_summary` now shows 2,259 sessions (up 10 from the prior pass, `claude-code` now 146, `opencode` now 213) — session count is climbing as expected, but `total_tokens_in`/`total_tokens_out`/`total_usd` are still exactly `0` across every backend with no exception. `eval_list_suites` still returns `[]`. Both gaps are confirmed persistent, not a stale one-time reading.
- **Re-verified 2026-09-30 (pass 4, see Verification log above):** `cost_summary` now shows 2,260 sessions (up 1), same 7 backends, still `0` for every token/USD field on every backend. `eval_list_suites` still `[]`. Fourth consecutive live check with the identical result — this is not a transient reporting glitch, it's the system's steady state.
- Combined, these two gaps mean the system has real-time introspection (this document is built entirely from live tool calls) but effectively no historical accounting — cost and quality trends cannot be reconstructed from stored data, only from re-querying live state.
- **Correction:** an earlier version of this document listed voice/audio transcription (`whisper.backend` empty) as a third observability/config gap alongside cost and evals. It isn't — see (d); an empty `whisper.backend` is the documented default that resolves to the local Whisper venv, and transcription is live and wired today. The two gaps above are the real ones.
