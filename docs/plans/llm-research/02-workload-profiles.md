# 02 — Workload Profiles

Re-verified live 2026-09-29 against the same datawatch instance (v8.34.1,
hostname `local-node`) documented in [01 — Current State](01-current-state.md),
via `get_config`, `autonomous_config_get`, `council_personas`,
`council_config_get`, `orchestrator_config_get`, `cost_summary`, `cost_rates`,
`capacity_status`, `compute_node_list`, `docs_search`/`docs_read`, and
`memory_recall`. Hardware/model/registry facts are carried over from 01, not
re-derived. Where a duration/token/cost figure could not be found live or in
memory, it's marked **unknown** rather than estimated.

**Correction pass (same day):** several "backend unset"/"no model pinned"
claims below were based only on MCP config dumps, not the datawatch Go
source. A follow-up pass read the actual source (`the datawatch repo root`,
HEAD `30c24271`, v8.35.1) and corrected profiles 4, 5, 6, and 8 in place —
see [01-current-state.md](01-current-state.md) for full citations.

**Goal this document serves:** keep Claude- and qwen3.8:27b-level quality
where a bad output silently corrupts downstream state, and route everything
else to smaller/faster models, so **8+ calls can run in parallel across
CPU+GPU** instead of serializing through one large model.

## Field legend

Input/output tokens (typical), context window (minimum needed), latency
tolerance (*interactive* = something is blocked waiting; *batch* = runs
unattended), quality bar (cost of a bad output), tool calling, structured
output, parallelism (how many instances usefully run at once), observed
cost/time (from `memory_recall`/`cost_summary`, else **unknown**).

## Workload profiles

### 1. PRD decomposition / planning (`decomposeFn`)

| Field | Value |
|---|---|
| Input / output tokens | Large (full request + project exploration) / medium-large (strict JSON PRD: stories+tasks) |
| Context window | Large — served today by `qwen3.8:27b` (~17.7 GB) |
| Latency tolerance | Batch — `autonomous.planning_timeout_seconds: 3600` (confirmed live) |
| Quality bar | **Catastrophic**: invalid JSON blocks the PRD entirely; valid-but-incoherent output wastes every downstream worker built from it |
| Tool calling | Yes — project exploration (file reads/listing); this call site has extra hardening: `<user_data>` wrapping + injection-guard scan (v8.18.0, confirmed) |
| Structured output | Yes, strict JSON — hard requirement, consumed programmatically |
| Parallelism | Low by design — `max_parallel_tasks: 3`, `node:datawatch` caps `max_concurrent_sessions: 1` (confirmed) → one decomposition runs at a time today |
| Observed cost/time | **Unknown.** `cost_summary` shows 0 tokens/cost on every backend (01(f)); no duration found via `memory_recall`. Highest-value gap to start measuring before any routing change. |

### 2. Coding worker sessions (opencode / claude-code)

| Field | Value |
|---|---|
| Input / output tokens | Large and growing (accumulated tool output) / medium per turn, many turns |
| Context window | Large, sustained; `ollama_chunk_timeout_sec: 1200` implies turns up to ~20 min |
| Latency tolerance | Batch, with a floor — `stale_task_seconds` stall detector fires at 1800s of *silence*, not per-turn (confirmed via memory: a worker stalled 30m25s) |
| Quality bar | **Catastrophic**: hallucinated file paths/APIs produce broken diffs the verifier (spec+diff only) may not catch |
| Tool calling | Yes, extensively — defining trait of this workload; weak tool-calling is disqualifying here |
| Structured output | No strict schema on model output; tool-call formatting must be reliable |
| Parallelism | Same ceiling as decomposition — `max_parallel_tasks: 3`; live `capacity_status` shows exactly one active lease on `node:datawatch` |
| Observed cost/time | **Unknown.** 210 opencode + 139 claude-code sessions recorded, all with zero tokens/cost (`cost_summary`, confirmed). Most expensive workload to be missing cost data for. |

### 3. Verifier (`autonomousVerify`)

| Field | Value |
|---|---|
| Input / output tokens | Small-medium, bounded: task spec + git diff capped at `verifier_diff_max_bytes` (0 = 8 KB, confirmed) / tiny (pass/fail + rationale) |
| Context window | Small |
| Latency tolerance | Interactive-ish — gates task-to-task progression inside `max_parallel_tasks: 3` |
| Quality bar | **Catastrophic on false-pass**: nothing downstream catches it today (`orchestrator.enabled: false`). False-fail is cheap (`auto_fix_retries: 1`) |
| Tool calling | No — pure text-in, judgement-out |
| Structured output | Yes, at minimum pass/fail flag |
| Parallelism | Up to `max_parallel_tasks: 3` — one verify per in-flight task |
| Observed cost/time | **Unknown**, but bounded 8 KB input makes this a strong small-model candidate regardless. **Confirmed gap:** with no git history or on a remote node, falls back to spec-only verification silently, no flag raised. |

### 4. Guardrail checks (`autonomousGuardrail`)

| Field | Value |
|---|---|
| Input / output tokens | Small-medium (scoped artifact: rules/security/release-readiness/docs) / small (pass/warn/block + reason) |
| Context window | Small |
| Latency tolerance | Interactive-bounded — `guardrail_timeout_ms: 120000` (2 min, confirmed), tightest explicit timeout in the system |
| Quality bar | **Catastrophic on false-pass for `security`** specifically — but currently moot: `orchestrator.enabled: false` (confirmed), whole subsystem off despite being pre-configured |
| Tool calling | Not confirmed either way in docs corpus |
| Structured output | Yes — pass/warn/block verdict stream |
| Parallelism | Up to `max_parallel_prds: 2` × up to 4 guardrails/PRD, once enabled |
| Observed cost/time | **Unobserved**, not just unknown — no live traffic since disabled. Falls back to the named `ollama` registry entry (`qwen3:1.7b`@`local-node`) via `amgrCfg.VerificationBackend`, same path as the verifier — see [01(c) drift item 6](01-current-state.md#c-llm-registry-and-drift) for why `orchestrator.guardrail_backend` is a separate, unread config key. |

### 5. Council persona proposals (12 personas)

| Field | Value |
|---|---|
| Input / output tokens | Medium (proposal + ~40-120 word role system prompt, optional vision-described image prepended) / medium (specific, cited critique — personas are written to "be specific") |
| Context window | Small-medium, no multi-turn accumulation per persona |
| Latency tolerance | Semi-interactive — async run, SSE-streamed, PWA shows live per-persona cards |
| Quality bar | **Recoverable, not catastrophic** — weak response degrades debate value only. Most failure-tolerant + most parallel workload profiled → strongest downsizing candidate |
| Tool calling | No |
| Structured output | No strict schema — freeform critique text |
| Parallelism | **Most naturally parallel workload in datawatch**: 12 independent calls/round, but `council.max_parallel: 0` means default **2** concurrent (confirmed via docs) — clearest concrete "raise this number" opportunity for the 8+ parallel goal |
| Observed cost/time | **Unknown.** `council.llm_ref` is empty (confirmed) — falls back deterministically to the named `ollama` registry entry, concretely `qwen3:1.7b`@`local-node` (`cmd/datawatch/main.go:3160-3163`; live `ollama.model`/`ollama.host` in config.yaml) — not an ambiguous default, but still the smallest general model in the fleet backing adversarial personas (`hacker`, `app-hacker`, `security-skeptic`) — see drift item 4, 01(c) |

### 6. Council synthesis (consensus + dissent)

| Field | Value |
|---|---|
| Input / output tokens | Largest of any Council call — all 12 persona responses concatenated / medium (consensus + dissent list) |
| Context window | Large relative to rest of Council Mode — must hold 12 full responses at once. **Should not** be downsized as aggressively as persona calls; losing nuance here undoes the point of running 12 personas |
| Latency tolerance | Batch-ish — once per round, off the critical path of persona parallelism |
| Quality bar | Dropping a dissenting point is a silent failure — Council Mode exists to surface disagreement, not manufacture false consensus |
| Tool calling | No |
| Structured output | Implied (consensus/no-consensus signal drives "milestone" comm pushes, confirmed) though not a strict schema |
| Parallelism | None — one synthesis call per round, sequential after the persona round |
| Observed cost/time | **Unknown**, same `council.llm_ref` fallback as profile 5 — deterministically `qwen3:1.7b`@`local-node`, not unset |

### 7. Vision description (image → short text)

| Field | Value |
|---|---|
| Input / output tokens | One image + short prompt (`vision.default_prompt` empty, confirmed) / small |
| Context window | Small — `moondream:latest`, 1B params (01(b)) |
| Latency tolerance | Interactive when invoked from comms router or Council image critique; batch-ish via the `vision_describe` MCP tool directly |
| Quality bar | Recoverable — vague description degrades a reply/critique, doesn't corrupt state |
| Tool calling | No (this is itself the tool other flows call) |
| Structured output | No — freeform short text |
| Parallelism | Call-by-call, gated by whichever flow invokes it |
| Observed cost/time | **Unknown.** Confirmed live, no drift since 01: `vision.enabled: true`, `backend: ollama`, `endpoint: http://localhost:11434`, `model: moondream:latest` |

### 8. Voice / speech-to-text (Signal, Telegram, Slack voice notes)

**Correction (same-day pass):** the original write-up below called this a
"backend unconfigured" gap because `whisper.backend` is an empty string. That
was wrong — empty is the documented default. `internal/config/config.go:1814-1816`
states `""/"whisper" (default) — local OpenAI-Whisper Python venv`, and
`internal/transcribe/factory.go:26-35` (`case "", "whisper", "local":`)
builds the real local-venv transcriber for it, explicitly to keep "existing
operator configs" working unchanged. Combined with live config —
`whisper.enabled: true`, `whisper.model: "medium"`, `whisper.venv_path`
set, `signal.enabled: true` — voice transcription is fully wired and active
today via the local Whisper venv on `local-node`. This is a real, running
workload, not a hypothetical one.

| Field | Value |
|---|---|
| Input / output | Short audio clips (seconds–minutes) / small text transcript proportional to clip length |
| Context window | N/A — a speech model, not run through the LLM registry/context path |
| Latency tolerance | Interactive — a voice note feels synchronous to the conversation that follows |
| Quality bar | Recoverable, user-facing and immediately noticed (closer to vision's failure mode) |
| Tool calling / structured output | N/A |
| Parallelism | Naturally high across independent comms channels; runs locally on `local-node` (CPU, via the Whisper Python venv — separate resource pool from the Ollama GPU workloads above) |
| Observed cost/time | **Unknown** — no per-transcription timing/cost surfaced in `cost_summary` or memory. Worth measuring before assuming it's cheap: local Whisper `medium` on CPU can be slower than the Ollama-served models above for longer clips. |

### 9. Memory embedding (nomic-embed-text)

| Field | Value |
|---|---|
| Input / output tokens | Tiny (individual memory entries/chat turns) / N/A — fixed-size vector, not generated text |
| Context window | Small — `nomic-embed-text`, 137M params, 0.3 GB (01(b)) |
| Latency tolerance | Interactive from caller's view, trivially fast per call |
| Quality bar | Silent-but-cumulative, not acute — degraded embedding slowly worsens `memory_recall` search, never crashes anything. Highest call-volume workload (`auto_hooks`, `auto_save`, `hook_save_interval: 15` all confirmed live) |
| Tool calling | No |
| Structured output | No — raw vector |
| Parallelism | Very high — tiny, independent, stateless calls. Strongest downsizing candidate alongside Council personas |
| Observed cost/time | Unknown per-call. Confirmed: runs on `local-node`, currently the **only** loaded model on that node (0.57 GB VRAM, confirmed via `/api/ps`) — the local 32 GB GPU sits almost entirely idle outside embedding traffic. Clearest evidence the hardware for the user's parallel-routing goal already exists and is underused. |

### 10. Memory fact extraction / session summarization

| Field | Value |
|---|---|
| Input / output tokens | Medium (accumulated session turns) / small-medium (summary or extracted facts) |
| Context window | Medium — currently served by `qwen3:1.7b` (`session.summarizer.llm_ref: "ollama-local-node"`, confirmed) — already the smallest general-purpose model in the fleet |
| Latency tolerance | Batch — runs on session end/checkpoint |
| Quality bar | Recoverable — degrades future recall quality only; raw content stays separately stored (`memory.storage_mode: "verbatim"`, confirmed) and can be re-summarized |
| Tool calling | No |
| Structured output | Likely yes for fact extraction; no strict schema confirmed for the summarizer in the docs searched |
| Parallelism | High — one per session ending, sessions end independently |
| Observed cost/time | Unknown. Already on the system's smallest model — a validation case for the routing goal, not a further downsizing target |

### 11. Interactive chat sessions (claude-code / opencode / ollama chat)

| Field | Value |
|---|---|
| Input / output tokens | Variable, user-driven / variable, human-consumed |
| Context window | As large as active backend supports; `session.llm_backend: "claude-code"` is active default (confirmed), model selectable (opus/sonnet/haiku) |
| Latency tolerance | Strictly interactive — human waits on every turn; the one workload where per-token latency, not just wall-clock, matters |
| Quality bar | Most failure-tolerant profiled — human in the loop catches and corrects immediately, no autonomous pipeline trusts it blindly |
| Tool calling | Yes when backed by claude-code/opencode; no for plain ollama chat |
| Structured output | No |
| Parallelism | Bounded by `session.max_sessions: 10`, `reserved_interactive: 1` (confirmed) — a concurrency ceiling on humans-in-sessions, not the target of "more parallel calls" |
| Observed cost/time | Unknown per-session; 139 claude-code sessions recorded with `usd: 0` (confirmed) — same broken cost accounting as every backend (01(f)) |

## Summary: routing into tiers

| Tier | Workloads | Why | Today's actual routing |
|---|---|---|---|
| **Fast-small** | Verifier, guardrail checks, memory embedding, fact extraction/summarization | Bounded input, cheap failure mode (except verifier's false-pass asymmetry), highest parallelism | Summarizer on `qwen3:1.7b`, embedding on `nomic-embed-text` — correct. Verifier + guardrail both fall back to the named `ollama` entry, also `qwen3:1.7b`@`local-node` — an *implicit but deterministic* pin, not silent drift (drift items 5-6, 01(c)) |
| **Balanced** | Council persona proposals, vision description, voice/STT | Needs a distinct, specific perspective or an accurate short description, but no catastrophic downstream trust in any single call | Vision correctly pinned to `moondream:latest`. Council personas fall back to the same `ollama`/`qwen3:1.7b`@`local-node` entry (`council.llm_ref: ""`, drift item 4) — deterministic, but arguably under-provisioned for adversarial personas. Voice/STT is live on local Whisper (`medium`), not a gap |
| **Strong** | Council synthesis, interactive chat sessions | Must reconcile 12 viewpoints fairly, or is directly human-facing | Interactive sessions correctly pinned to `claude-code`. Synthesis shares Council's `qwen3:1.7b` fallback |
| **Frontier / agentic** | PRD decomposition/planning, coding worker sessions | Strict-JSON reliability and reliable multi-turn tool calling are non-negotiable; the only two call sites with explicit v8.18.0 injection-guard hardening | Both correctly pinned to `qwen3.8:27b` via opencode on `datawatch` — the one place today's routing already matches the target end-state |

**Net picture (corrected):** the tiers that most need to stay strong
(frontier/agentic) are already correctly pinned. The gap is narrower than
first described: verifier, guardrail, and both Council call sites are **not**
unpinned — they all resolve, in source, to one shared named fallback
(`ollama` → `qwen3:1.7b`@`local-node`). The real finding is that this one
small model is doing quadruple duty (verifier + guardrail + council persona
+ council synthesis) with no config distinguishing them, and `council.
max_parallel: 0` (→ default 2) caps the system's single most parallel-shaped
workload well below its natural 12-wide structure. Getting to "8+ calls in
parallel across CPU+GPU" is less about finding new small models —
`local-node`'s 32 GB GPU already sits mostly idle outside embedding traffic
(profile 9) — and more about (a) deciding whether the four fast/balanced
call sites sharing one fallback model is fine or should be split out with
explicit `*_backend`/`llm_ref` keys, and (b) raising `council.max_parallel`
and `node:datawatch`'s `max_concurrent_sessions: 1`, both confirmed live as
today's actual binding constraints rather than any model being too slow.
