# 06 — Benchmark Plan

**Goal:** before any candidate from [03 — Text Model Candidates](03-text-model-candidates.md)
or [04 — Multimodal Candidates](04-multimodal-candidates.md) replaces an
incumbent in [08 — Recommendations](08-recommendations.md)'s routing table,
get real evidence — quality *and* throughput — using datawatch's own
rubric-based Evals feature plus raw Ollama performance fields. Today there is
none: `eval_list_suites` returns `[]` (re-confirmed live 2026-09-30) and
`cost_summary` shows 0 tokens/cost across every backend (01(f), 07 Part B,
08). Every recommendation in 03/08 is a quality-*shape* argument, not a
measured result. This document is the plan to close that gap, not the
results themselves.

**Re-verified live 2026-09-30** via `get_version` (8.36.10, 8.36.11
available — daemon has moved five times since 05's 8.36.7 snapshot, same
day), `eval_list_suites` (`[]`, unchanged), `llm_list`, `compute_node_list`,
`ollama_stats`. No drift in registry/node facts since 03/05: same four
compute nodes (`datawatch` priority 90 no declared capacity, `datawatch-openai`
priority 90 with the declared B200, `local-node` priority 70 with the RTX
32 GB GPU, `test-node` stale duplicate), same five LLM registry entries, same
four models actually resident on local `local-node` Ollama (`moondream`,
`llmvision/glimpse-v1`, `qwen3:1.7b`, `nomic-embed-text`, 7.8 GB total).

**Re-checked a second time, same day (2026-09-30), on a retry pass**: still
`get_version` 8.36.10/8.36.11, still `eval_list_suites: []`, still the same
four compute nodes and the same four models on `local-node`. One new data
point worth recording: `cost_summary`'s session count climbed from 2243 (the
task brief's snapshot) to 2298 in the interval between the two checks, while
`total_tokens_in`/`total_tokens_out`/`total_usd` stayed exactly `0` across
every backend (`claude-code`, `opencode`, `ollama`, `subprocess`, etc.) —
55 more sessions logged with zero token accounting confirms 01(f)/07 Part
B's "broken, not just empty" framing; this isn't a cold registry that simply
hasn't been used yet.

## What "eval sweep" already means in this codebase — and why it isn't usable yet

`memory_recall("eval sweep")` surfaces an in-progress datawatch feature, not
a finished one. A 2026-09-16 session in the `datawatch` source repo
(`docs/plans/harness-impl/eval-sweep-api.md` and
`eval-sweep-technical-spec.md`, referenced but not re-read here — out of
this project's read-only, docs-under-`llm-research` scope) was implementing
**"Eval Sweep (P1 sweep-core)"**: new MCP tools `eval_sweep` /
`eval_list_runsets` / `eval_get_runset`, matching `R1/R2/R3` REST routes,
CLI verbs, comms verbs, and YAML config, all meant to run a **suite × model
× node matrix** in one call (`RunSet` grouping many `Run`s via
`Run.ParentRunID` and a `kind:runset` filter) instead of one `eval_run` at a
time. The checkpoint trail shows real progress — existing code read
(`internal/evals/evals.go`, `internal/mcp/evals.go`,
`internal/server/evals.go`, `cmd/datawatch/cli_evals.go`), a todo list with
items still unchecked: adding `TokensIn`/`TokensOut` to `inference.Response`
(a P0 blocker for per-model cost/token accounting in a sweep — the same gap
07 Part B/08 flag as absent fleet-wide), the `SweepRunner` fan-out/settle
logic, and the boot wiring in `main.go`.

**This session's live tool list confirms it never shipped**: only
`eval_run`, `eval_list_runs`, `eval_get_run`, and `eval_list_suites` exist
today — no `eval_sweep`, `eval_list_runsets`, or `eval_get_runset`. Treat the
eval-sweep design as **a real, citable prior proposal to build toward**, not
a tool this plan can call. Until it ships, "sweeping" a suite across
models/nodes means scripting repeated `eval_run` calls externally (§4) and
diffing `eval_get_run` results by hand — exactly the gap `Run.ParentRunID`
was meant to close.

## 1. What "Evals" gives us today

Per `docs_read howto/evals.md` and `datawatch-definitions.md#settings-automate`
(both re-read live this session): a suite has a `mode` (`capability` or
`regression`) with a pass-threshold, and a list of graders scoring output
against a rubric. Default suite **types**: `string_match`, `regex_match`,
`binary_test`, `llm_rubric`. Suites live under `~/.datawatch/evals/`
(outside this project's write scope — described here, not authored), results
under `~/.datawatch/evals/runs/`, retrievable via `eval_get_run`. Evals also
feeds Algorithm Mode's Measure phase, so a suite built for this plan is
reusable there later, not single-purpose.

## 2. Proposed suites, mapped to real workloads (from 02)

Each suite below is scoped to mirror one [02](02-workload-profiles.md)
profile, using that profile's own stated quality bar to decide how strict
the grader needs to be. None of these exist under `~/.datawatch/evals/` yet
— this is a design, per the task's scope (research/documentation only, no
YAML authored outside this repo).

### Suite 1 — PRD decomposition validity (`prd-decompose`, profile 1)
- **Type:** `binary_test` (schema/shape checks) chained with `llm_rubric`
  (coherence check).
- **Inputs:** 3-4 real feature requests, not synthetic one-liners — profile
  1's failure mode (02) is *valid-but-incoherent* JSON, which only shows up
  under realistic complexity.
- **Grading:** `binary_test` — valid JSON; `stories[]` each with `tasks[]`;
  every `depends_on` references a real task ID; every `files` path is
  plausible (no invented paths, none outside the target repo). `llm_rubric`
  — story count proportionate to scope, tasks concrete enough to act on.
- **Pass threshold:** 100% on shape checks (02: "catastrophic" on invalid
  JSON, no partial credit); ≥80% on the rubric check.

### Suite 2 — Verifier judgement accuracy (`verifier-judge`, profile 3)
- **Type:** `binary_test`.
- **Inputs:** a hand-labeled set of (task spec, git diff, expected verdict)
  triples — some diffs that should clearly pass, some clearly fail (wrong
  file touched, test regression, empty diff) — built from real repo history
  so `git diff` looks like production input, not a toy.
- **Grading:** model's verdict must match the labeled verdict exactly.
- **Pass threshold:** per 02, false-pass is "catastrophic," false-fail is
  "cheap" (auto-fix retry exists) — score false-positive/false-negative
  rates *separately*, and require false-pass near-zero even at the cost of
  overall accuracy.

### Suite 3 — Guardrail / security classification (`guardrail-classify`, profile 4)
- **Type:** `binary_test` (verdict match) — `string_match`/`regex_match` if
  the model must also emit a fixed-format reason string.
- **Inputs:** a labeled set of PRD/task specs spanning the guardrail's
  scoped artifacts (rules/security/release-readiness/docs, 02 profile 4),
  including adversarial/injection-flavored inputs — this call site has
  `<user_data>`-wrapping and injection-guard hardening (v8.18.0, confirmed
  via `datawatch-definitions.md#settings-automate`).
- **Pass threshold:** near-zero false-pass on the `security` scope, mirroring
  02's stated asymmetry for this profile.

### Suite 4 — Council persona critique quality (`council-persona`, profile 5)
- **Type:** `llm_rubric`.
- **Inputs:** 3-5 real-representative proposals, run through each of the 12
  personas (or a subset if cost-bound — §5).
- **Grading rubric:** role adherence (does `security-skeptic`'s critique
  actually read security-flavored, not generic?), specificity (02: personas
  are written to "be specific" — penalize boilerplate), no duplicating
  another persona's stock angle.
- **Pass threshold:** ≥80% — 02 rates this "recoverable, not catastrophic,"
  the most failure-tolerant profile surveyed, so it can accept a lower bar
  in exchange for testing more model/persona combinations per run (§5).

### Suite 5 — Tool-calling reliability (`tool-calling-agentic`, profile 2)
- **Type:** `binary_test` against ground truth, run through `opencode` (the
  actual interface profile 2 uses) rather than a bare chat call — a model
  that scores well on a text-only tool-calling benchmark can still fail
  inside opencode's own harness.
- **Inputs:** a short, bounded agentic task with a known-correct outcome
  (e.g. "add a field to this struct and update its two callers" against a
  small fixture repo) — small enough to finish well under
  `ollama_chunk_timeout_sec: 1200` (02).
- **Grading:** did the diff touch exactly the expected files, with no
  hallucinated paths/APIs (02's stated failure mode) — checkable
  mechanically, not by rubric.
- **Pass threshold:** 100% file-path correctness (the named catastrophic
  failure mode); rubric-free by design, so this stays cheap to run per
  candidate model without an `llm_rubric` judge call.

### Suite 6 — Vision captioning (`vision-caption`, profile 7)
- **Type:** `llm_rubric`.
- **Inputs:** 2-3 UI screenshots and at least one architecture diagram (per
  02, Council Mode's `image_path` critique feature is the real consumer of
  this path) — mixed content on purpose, since a caption model tuned for
  photos can underperform on diagrams with text/arrows.
- **Grading rubric:** factual accuracy (no invented UI elements), whether
  diagram labels/relationships are actually read and described, length
  proportionate to `vision.default_prompt` being empty (02) — i.e. does the
  model produce a useful default-length description with no prompt steering.
- **Pass threshold:** ≥80%, consistent with profile 7's "recoverable"
  quality bar (02).

### Suite 7 (conditional) — Speech-to-text WER (`stt-wer`, profile 8)
- **In scope** because 05 (§ voice pipeline) confirms live
  `whisper.enabled: true`, `model: "medium"`, a real running pipeline — not
  the hypothetical the original brief worried it might be.
- **Type:** `string_match`/`regex_match` on a normalized transcript compared
  against a reference transcript, with WER computed externally (word error
  rate is an edit-distance calculation, not a suite-native grader type) and
  logged alongside the suite's pass/fail record.
- **Inputs:** a handful of short recorded clips with known-correct
  transcripts, ideally recorded on the same channel path (Signal/Telegram
  voice note) actual traffic uses, not clean studio audio, since 02 profile
  8 rates this workload "user-facing and immediately noticed" — WER on
  realistic noisy audio is what actually matters.
- **Pass threshold:** track WER as a number, not pass/fail — this suite is
  for trending a metric (does a candidate STT model's WER regress vs.
  Whisper-medium's baseline?) more than gating a binary decision.

## 3. Performance metrics to capture (separate from quality suites)

Quality suites above answer "is the output good enough." These metrics
answer "is it fast/cheap enough" — both are required before any 08 routing
change, since 03/08 explicitly note every existing recommendation is
untested on either axis.

| Metric | How to read it |
|---|---|
| Time-to-first-token (TTFT) | Not directly exposed by non-streaming `/api/generate`; approximate as `prompt_eval_duration` (prefill time) when `stream:false`, or measure wall-clock-to-first-chunk with `stream:true` for an exact number. |
| Decode tokens/sec | `eval_count / (eval_duration / 1e9)` — both fields are nanoseconds, confirmed live (§4). |
| Prefill tokens/sec | `prompt_eval_count / (prompt_eval_duration / 1e9)`. |
| Total latency | `total_duration / 1e9` — **includes queueing**, not just compute; §4 shows this matters in practice on a shared node. |
| Model load overhead | `load_duration / 1e9` — near-zero when the model is already resident (warm), large on a cold call (§4). |
| Throughput at concurrency 1/4/8 | Issue N concurrent `/api/generate` calls against the same already-loaded model and divide total output tokens by wall-clock time for the batch; compare against `OLLAMA_NUM_PARALLEL` (05(a), default 1 — concurrency above that queues inside Ollama rather than truly parallelizing, so confirm the server's actual `OLLAMA_NUM_PARALLEL` before trusting a concurrency-4/8 number as real parallelism). |
| VRAM / RAM used | `ollama_stats`/`/api/ps` for VRAM per loaded model (already used in 01/03/05); `free -h` for system RAM. |
| GPU utilization | `nvidia-smi` — **blocked on `local-node` today** by the driver/library version mismatch documented in [05(g)](05-serving-and-hardware.md#g-prerequisite-local-nvidia-smi-driverlibrary-mismatch); fix that first if local-GPU utilization numbers are needed, or read `datawatch`'s own GPU via whatever access that host allows (not checked in this pass — out of scope). |

## 4. Observed timing — seed data (this session, 2026-09-30)

Ad-hoc, read-only `POST /api/generate` calls against `http://datawatch:11434`
using already-installed models, per this task's explicit allowance. Exact
prompt for every call: `"List three uses of a bicycle in one short
sentence."`, `stream:false`, `options.num_predict` as noted. Raw fields
below are copied directly from the JSON response, not recomputed or
estimated.

| Model | State | `load_duration` (s) | `prompt_eval_count` | `prompt_eval_duration` (s) | `eval_count` | `eval_duration` (s) | `total_duration` (s) | Decode tok/s |
|---|---|---|---|---|---|---|---|---|
| `qwen3:8b` | cold load | 4.23 | 21 | 0.225 | 60 | 2.18 | 6.64 | ~27.5 |
| `qwen3.8:27b` | warm (already loaded) | 0.004 | 21 | 1.14 | 30 | 2.82 | **21.70** | ~10.6 |

**`qwen3:8b`'s `response` field came back empty** despite `eval_count: 60` —
consistent with qwen3's default "thinking" mode consuming the `num_predict`
budget on `<thinking>` tokens before emitting final text (`qwen3.8:27b`'s
raw response, by contrast, shows a `"thinking"` field populated and
`response: ""` too — same pattern, both need a higher `num_predict` or
`think:false` in the request to get real output text; noted as a
methodology fix for the next pass, not a benchmarking blocker since timing
fields are populated either way).

**`qwen2.5:14b` and `Gemma3:12b` both failed to respond within 90s and 60s
respectively** (`curl` exit 28, connection timeout) despite being installed
and previously loadable per 01/03. `/api/ps` checked immediately after shows
only `qwen3.8:27b` resident — meaning something else (real production
traffic on this shared, live-monitoring instance, not this benchmarking
session) was loading/using the node concurrently. **This is itself a
finding, not just a methodology footnote:** `qwen3.8:27b`'s own
`total_duration` (21.7s) is far larger than `prompt_eval_duration +
eval_duration + load_duration` (≈4.0s) can account for — the ~17.7s gap is
almost certainly queueing behind other traffic on `node:datawatch`'s
`max_concurrent_sessions: 1` (05(f), 08). **Any benchmark run against
`datawatch` while it's serving live traffic will conflate queueing time with
model speed** unless isolated — see the run-order note in §5.

## 5. Results template

| Model | Node / engine | Suite | Quality score | Concurrency | Decode tok/s | TTFT (approx.) | VRAM (GB) | vs. incumbent | Decision |
|---|---|---|---|---|---|---|---|---|---|
| *(incumbent)* `qwen3.8:27b` | `datawatch` / Ollama | `prd-decompose` | *(baseline, = 100%)* | 1 | — | — | 15.5 (confirmed) | — | keep (baseline) |
| candidate A | | | | 1 / 4 / 8 | | | | % of incumbent quality; ×N throughput | |

Fill one row per (model, suite, concurrency) combination actually run;
leave cells blank rather than estimated when a metric wasn't measured —
matching every other document in this series' rule of marking gaps
explicitly (01, 02, 03).

## 6. Decision rule

A candidate model **replaces the incumbent for a workload tier** only when,
on that tier's mapped suite (§2) at the tier's real target concurrency:

1. **Quality score ≥ 90% of the incumbent's score** on the same suite (not a
   different, easier one) — except Suites 1-3 (PRD, verifier, guardrail),
   where 02 calls the failure mode catastrophic; for those, the relevant
   `binary_test` shape/false-pass checks must stay at parity (no regression
   tolerance), and only the softer `llm_rubric` component gets the 90%
   allowance.
2. **Throughput ≥ 2× the incumbent's decode tok/s** at that tier's real
   concurrency — not concurrency 1, since 02/05's whole argument for
   downsizing (verifier, guardrail, Council personas, embedding) is about
   enabling more *parallel* calls, and a model that's only faster
   single-stream doesn't address that goal.
3. **Both conditions on the same measurement pass**, ideally the same
   session, to avoid comparing a quality run from one day against a
   throughput run from another on a node whose queueing behavior (§4) has
   already been shown to vary with unrelated live traffic.

A candidate meeting only one condition is **not a swap** — flag it in the
results template as a fallback/challenger (matching how 03/08 already
qualify their own unmeasured recommendations) and re-test later rather than
guessing which axis matters more.

## 7. Run order — cheapest first

1. **Fix the measurement environment before measuring anything.** Confirm
   `OLLAMA_NUM_PARALLEL` on `datawatch` (needed to interpret any
   concurrency-4/8 number, §3) and note whether the shared node has a
   low-traffic window — §4's timeouts show contention is real, not
   hypothetical, on this instance.
2. **`binary_test`/`string_match`/`regex_match` suites on already-installed
   models first** (Suites 1, 2, 3, 5 have mechanical graders) — no
   `llm_rubric` judge call needed, so these are the cheapest to iterate on
   and the fastest to catch a broken suite definition before spending an
   `llm_rubric` budget on it.
3. **`llm_rubric` suites next** (4, 6) — each grading call is itself an LLM
   call, so these cost roughly 2× (subject + judge) per item; run only after
   the mechanical suites confirm the harness/fixtures are correct.
4. **Concurrency 1 throughput numbers before 4 before 8** (§3) — concurrency
   1 isolates raw model speed from queueing/batching effects; only move to
   4/8 once 1 is understood, so a contention artifact like §4's isn't
   misread as a model-speed finding.
5. **Only pull a new model (not yet installed) after the harness is proven**
   on already-installed models — per this task's scope, no new model is
   pulled in this pass; the first real "new candidate" run should be a
   single fast-small candidate (e.g. Qwen3-4B-Instruct-2507, cheapest to
   download and run per 03) evaluated against Suites 2/3 (verifier/guardrail
   — mechanical graders, cheapest suites) before any larger/MoE candidate.
6. **Full suite × model × node matrix last**, and only automatically once
   the eval-sweep feature (see above) actually ships `eval_sweep`/
   `Run.ParentRunID`; until then, treat "sweep" as a manually scripted loop
   over `eval_run` and budget accordingly — every additional model or node
   multiplies both wall-clock time and, once token accounting is fixed
   (07 Part B), real inference cost.

## 8. Known gaps this plan cannot close by itself

- **No suites exist yet** (`eval_list_suites: []`, confirmed live) — §2 is a
  design to author under `~/.datawatch/evals/`, not a completed artifact.
- **`eval_sweep` is unshipped** — see the dedicated section above; running
  more than one model/suite combination today means external scripting.
- **Token/cost accounting is broken fleet-wide** (`cost_summary` all-zero,
  01(f)/07 Part B/08) — "cost per candidate" cannot be measured until that's
  fixed; throughput and quality are the only axes this plan can use today.
- **`node:datawatch` contention is real and was directly observed** (§4) —
  any benchmark run against it should either request a maintenance window or
  explicitly report queueing-vs-compute time separately, or risk attributing
  shared-traffic slowdowns to a candidate model.
- **Local GPU telemetry is blocked** on `local-node` by the `nvidia-smi`
  driver/library mismatch ([05(g)](05-serving-and-hardware.md#g-prerequisite-local-nvidia-smi-driverlibrary-mismatch))
  — VRAM numbers from `ollama_stats`/`/api/ps` remain usable; GPU
  utilization percentages do not, until that's fixed.
