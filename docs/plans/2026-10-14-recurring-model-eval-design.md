# BL437 — Recurring hardware-aware model/effort eval (design pass)

**Status:** needs_review · **Date:** 2026-10-14 · **Tracker:** BL437
**Scope of this doc:** design only. No Go or Python implementation, no default model/effort/backend changes, no mobile-app changes, no pulls, no config writes. Client-visible surfaces are listed as proposals only.

---

## Context and evidence

### What exists today

| Piece | Where | What it actually does |
|---|---|---|
| BL422 eval fixtures | `testdata/eval/model-tiering/README.md`, `matrix.json`, `rates.json` | 4 task types × 13 execution tiers = 52 cells, plus 13 decompose cells. Verifier is per-cell `verify/<type>.sh`, exit 0 = pass. Verification tier is deliberately *not* a fixture; it needs a guarded global-config flip (README "Touchpoints covered"). |
| BL422 drivers | `scripts/eval-model-tiering.sh`, `scripts/eval_model_tiering.py` | Driver for the fixtures above. Claude-only today. |
| BL422 run dir | `docs/testing/runs/2026-05-16-001/evidence` | Only a dated evidence folder; no model-tiering results are checked in. Treated as "no results yet". |
| BL259 eval framework | `internal/evals/evals.go:1-20, 39-46, 182-210`; `internal/server/evals.go:1-11`; `internal/mcp/evals.go:4-7`; routes `internal/server/server.go:354-358` | Suite/Case/Grader/Run types, suites loaded from `~/.datawatch/evals/<name>.yaml`, runs persisted to `~/.datawatch/evals/runs/<id>.json` (path from the package doc comment; **not read**, per scope). REST `/api/evals/*`, MCP `eval_list_suites`, `eval_run`, `eval_list_runs`, `eval_get_run`. |
| **BL259 graders** | `internal/evals/evals.go:280-298` | `string_match`, `regex_match`, `binary_test` are deterministic. **`llm_rubric` always returns `Pass:false`, with feedback "manual review needed (LLM grading shipped in v6.10 follow-up)".** BL259 cannot grade model output with an LLM today. |
| BL259 execution model | `internal/evals/evals.go:182-210` | `Execute` only grades pre-supplied `input`/`expected` strings. It does not call any model or backend. |
| Marketplace catalog | `internal/server/marketplace.go:152-168`; MCP `internal/mcp/compute.go:223` | Embedded, hand-curated list. Returns `"source":"embedded","refreshable":false`. Live MCP call returned **11 entries, 30 size tags** (llama3.1, llama3.3, qwen3, qwen2.5-coder, gemma3, phi4, deepseek-r1, mistral, mixtral, codellama, nomic-embed-text). The comment at marketplace.go:161-163 says a live scrape is future work. |
| Model pull | `internal/mcp/compute.go:237`; REST `POST /api/compute/nodes/<n>/models/pull` at `internal/server/marketplace.go:193-239` | Starts a background `ollama pull` goroutine and returns a `task_id`; progress polled at `/api/marketplace/ollama/tasks/<id>`. This is the real pull path. |
| Model refresh | `internal/server/inference.go:481-490` | **Stub.** `llm_refresh_models` audits the call and returns `"note":"refresh queued"`, but nothing is queued or refreshed. Do not rely on it for discovery. |
| Scheduler | `internal/session/schedule.go:28` (`SchedTypeSpawn`), `:84-85` (`CronExpr`), `:646-676` (cron re-arm); MCP `schedule_spawn` at `internal/mcp/server.go:1741`; `schedule_add` at `server.go:1716` | Two types. `command` (default) sends text into an existing session. `spawn` starts a fresh ephemeral session on a time or cron. Both accept `cron_expr`. |
| BL427 dispatcher | `internal/autonomous/lifecycle_event.go` (Phase 1, v9.0.85, per `docs/plans/README.md:1241-1247`) | In-process, status-event pub/sub (`Manager.Subscribe`). **No time/cadence trigger.** |
| BL431 trigger | `docs/plans/2026-10-14-automation-orchestration-design.md:899-930` | Automation coordinator subscribes to terminal PRD events (`prd.completed`, `prd.blocked`, `prd.failed`, `prd.cancelled`). **Event-driven, not cadence-driven.** |
| BL428 ceilings | `docs/plans/2026-10-14-concurrency-and-budget-design.md`: §3b `:321-360`, §3c `:361-437`, §4d `:514-632`, §4e `:633-689` | Section details are in the cost and capacity section below. |
| BL431 model defaults | `docs/plans/2026-10-14-automation-orchestration-design.md:853-898` (§6, "Multi-model / cost-tier defaults") | Automation-level model/effort tier sits between PRD and global. BL422's results would be applied here, one Automation at a time. |

### Live inventory (read-only MCP calls, 2026-10-14 ~21:47 local)

**`compute_node_list`** (datawatch MCP, the local daemon):

| Node | Kind | Address | Declared capacity | Declared hardware | Notes |
|---|---|---|---|---|---|
| `datawatch` | ollama | `http://datawatch:11434` | `{}` | `{}` | **No hardware declared at all** — no GPU, no CPU, no RAM. `max_concurrent_sessions:1`, priority 90. |
| `datawatch-openai` | openai-compat | `http://datawatch:3000` | gpus 1, 128 GB, max 3 models | nvidia `blackwell-b200`, count 1 | Served via OpenWebUI. |
| `local-node` | ollama | `http://local-node:11434` | gpus 1, **32 GB**, max 10 models | nvidia `gpu-32gb`, count 1 | **No CPU fields declared.** |
| `verdict` | ollama | `http://127.0.0.1:11435` | gpu_mem 32 GB, max 2 models | `{}` | Free-text tag `shared-gpu-with-local-node`; capacity ledger does not read it (BL428 §3c, `2026-10-14-concurrency-and-budget-design.md:361-380`). Serves the imap-mcp verdict workload. |

**`llm_list`** — registered backends and the models each one exposes. Examples:
- `claude-code` (kind claude-code): models opus, sonnet, haiku on node `claude`. `default_effort:"normal"`.
- `ollama-local-node` (node local-node): qwen3:1.7b, qwen3.5:35b, qwen3-coder:30b, qwen2.5:14b, Gemma3:12b, qwen3:8b, nomic-embed-text.
- `ollama-datawatch` (node datawatch): the widest set, including gpt-oss:120b, qwen3-coder-next:q4_K_M, qwen3.8:27b. **These sizes are registered but the node has no declared hardware, so fit cannot be checked from the registry.**
- `ollama-verdict` (node verdict): gemma4:26b, gpt-oss:20b, granite4.2:30b, granite4.2:8b.
- `openwebui-datawatch` (node datawatch-openai): qwen3-coder-next, gpt-oss:120b, and the Qwen/Gemma set.

**`capacity_status`** — pools: `host` (limit 9), `llm:claude-code` (**limit 0 = unlimited**), `node:datawatch` (limit 1), `node:datawatch-openai` / `node:local-node` / `node:verdict` (limit 0 = unlimited). Two live leases, both on `llm:claude-code`. This confirms BL428 §3b's "unlimited by default" finding.

**`ollama_stats`** — **not available.** The MCP call returned `404 unknown tool: ollama_stats` on this server. The tool appears in the listing but is not served. Live GPU/VRAM figures were therefore taken from the host directly (below).

### Hardware, local-node host — measured, not declared

Read-only on this host (`hostname` = `local-node`):

- **CPU:** AMD Ryzen Threadripper 9960X, **1 socket, 24 physical cores, 2 threads per core = 48 logical CPUs** (`nproc` = 48; `lscpu` NUMA node0 = 0-47).
- **RAM:** 122 GB total, 99 GB available (87 GB buff/cache).
- **GPU:** a 32 GB GPU, **32,607 MiB**, driver 595.91.07.

**Discrepancy to resolve:** the operator's stated figure for this host is **64 CPU cores**. The measured figure is **24 physical / 48 logical**. This doc uses the measured numbers. If the 64-core figure refers to a different machine, that node needs its own registration and measurement. Open question 1.

**CPU is not in the registry for any node.** `local-node` declares a GPU and nothing else. A CPU row therefore cannot be filtered from the registry today; the hardware probe in the implementation PRD must add it (see the follow-up PRD section, S2).

### Verdict node

`verdict` shares GPU memory with `local-node` per its free-text tag (BL428 §3c). It also serves the production imap-mcp verdict workload (`ollama-verdict` tags: `imap-mcp`, `verdict-model`, `spam-phishing-eval`). **This doc excludes `verdict` from every sweep cell.** Benchmarking there could degrade a live spam classifier, and the two nodes cannot run load independently.

---

## Discovery loop

**Goal:** each monthly run looks for models that are new since the last run, and only those.

### Sources (in order)

1. **Catalog diff.** Call `marketplace_ollama_catalog` and diff entry names and tags against the previous run's snapshot. Limitation: the catalog is embedded and `refreshable:false` (`marketplace.go:161-167`), so it only changes when the binary changes. It will not surface new models on its own. Treat it as a curated shortlist, not a live feed.
2. **Operator-named candidates.** A list in the sweep config (`eval.sweep.candidates`), owned by the operator. This is the main source until the catalog is live.
3. **Live Ollama library scrape.** Future work. The catalog comment says so (`marketplace.go:161-163`). Out of scope here; listed as open question 6.
4. **Registered-but-unevaluated models.** Diff `llm_list` / `llm_list_models` against prior runs. This finds models already on a node that have never been swept (e.g. `qwen3.5:35b` on `datawatch`).

### Pulling candidates

- Pull only through `compute_node_pull_model` (MCP) or `POST /api/compute/nodes/<n>/models/pull`, and poll `/api/marketplace/ollama/tasks/<id>`. Do not shell out to `ollama pull` (the existing path is audited and tracked).
- **Pulling is a discovery-loop action that needs operator confirmation** for each new model, because it changes disk usage on the node. The sweep may not pull on its own in v1.
- `llm_refresh_models` is **not** used: it is a stub (`inference.go:481-490`).

### Bound on candidates per run

| Bound | Value | Justification |
|---|---|---|
| New Ollama candidates per run | **≤ 2** (plus registered-unevaluated models, ≤ 3 in total) | Each candidate adds a full hardware-class sweep (see Test matrix). Two new candidates on two hardware classes is already about 40 local cells. More than that can't be reviewed by one operator in a month. |
| New CPU-only candidates per run | **≤ 1** | CPU inference is slow (see the CPU discovery subsection). One candidate is enough to make a meaningful data point. |
| Claude candidates per run | **0 new**, unless Anthropic publishes a new model id. Then **≤ 1**, with the effort grid reduced (see Test matrix). | BL422 already covers the 4 × 3 grid. |
| Wall-clock ceiling per run | **8 h** | Matches an overnight window. Anything past 8 h is recorded as `timed_out` and is not auto-retried. |
| Cell ceiling per run | **≤ 60 cells** | Set so the run finishes inside the wall-clock bound and the results fit one review. |
| Dollar ceiling for Claude cells | **Operator-set, default proposal $25 per run** | A proposal, not a measured value. Enforced against `cost_usage` before each Claude cell. Open question 3. |
| Local cost | Electricity and wall-clock only | Not billed, but the 8 h ceiling still applies because it blocks the GPU/CPU for other work. |

### CPU-only candidate discovery (first-class, not an afterthought)

CPU-only inference is its own hardware class (see Test matrix, row H4). The discovery loop must therefore also choose CPU candidates, and it does so by RAM and core budget, not by VRAM.

**Realistic model sizes for a 24-core / 48-thread CPU (no GPU):**

- Third-party guidance (not verified on this host; Ollama's own docs were not retrieved in this pass): CPU inference "works for small-to-mid models, but expect single-digit to low-double-digit tokens per second", a 7B q4_K_M model runs at about 5–15 tokens/s on an 8-core chip, and CPU use generally needs about 2× the RAM a GPU would ([valebyte.com](https://valebyte.com/en/blog/ollama-server-requirements-ram-vram-and-cpu-by-model-size/), [computingforgeeks.com](https://computingforgeeks.com/install-ollama-rocky-ubuntu/)). These are rough estimates; the sweep must measure, not assume.
- **Well suited (1.5B–8B, q4):** qwen3:1.7b, qwen3:8b, qwen2.5-coder 1.5b/7b, gemma3 1b/4b, deepseek-r1 1.5b/7b-8b. Of these, **qwen3:1.7b and qwen3:8b are already registered on `ollama-local-node`**; the others are catalog-only.
- **Usable for batch runs (≈14B):** qwen2.5:14b and Gemma3:12b are registered on local-node; phi4 14b and deepseek-r1 14b are catalog entries. Expect slow generation; fine for a single overnight sweep, not for interactive work.
- **Exclude from CPU (≥ 32B):** qwen3.5:35b, qwen3-coder:30b, granite4.2:30b, gpt-oss:120b, llama3.3:70b, deepseek-r1:70b. RAM would fit (122 GB), but generation would be too slow for the wall-clock bound.

**Does the CPU row need a new model pull?** **No, not for the first data point.** `qwen3:1.7b` and `qwen3:8b` are already on local-node's Ollama and fit the CPU budget. One new CPU candidate per month can then be pulled through the normal discovery path if the operator confirms it. **This pass pulls nothing.**

**How to run a CPU-only row on a GPU-equipped node** (local-node): Ollama takes GPU visibility from its environment at process start. Two options:

- (a) **Preferred:** a *second* Ollama server on another port, started with GPU visibility disabled (`CUDA_VISIBLE_DEVICES=""`), with its own models directory pointing at the same blobs. Register it as a separate compute node `local-node-cpu`, declared `cpu_cores_physical:24, cpu_threads:48, ram_gb:122, gpus:0`. It cannot disturb the primary GPU server, and it makes the CPU row explicit in the registry.
- (b) **Not recommended:** restart the primary `local-node` Ollama with GPU disabled for the sweep window. That stops live GPU inference for the duration and risks a half-restored GPU server.

Option (a) needs a service change on local-node. That is an operator action; this doc does not perform it.

---

## Test matrix

### Dimensions

- **Model:** the set of models available per backend (above).
- **Effort:** `low | medium | high` (xhigh/max excluded, as in BL422). **Effort applies only to `claude-code`.** Ollama has no equivalent. For local models the second axis is quantization (`q4_K_M` vs. whatever is registered) and context length, and both are fixed per cell, not swept.
- **Backend / driver:**
  - `claude-code` (Claude models, effort-capable)
  - `goose` (driver for Ollama/OpenWebUI models)
  - `opencode` (driver for Ollama/OpenWebUI models)
  - `ollama` / `openwebui` (direct backend, the target of goose/opencode)
  - `datawatch-default` (no override; what the daemon resolves for a PRD with no model set)
- **Hardware class:** see the table below. **The CPU-only row is its own class, distinct from every GPU VRAM tier.**

### Hardware classes

| Class | Definition | Where it exists today | Used for |
|---|---|---|---|
| **H0** `hosted` | Anthropic-hosted; no local hardware | `claude-code` | Claude cells. Not a local hardware class; excluded from hardware-conditioned local advice. |
| **H1** `gpu-32g` | Single GPU, 32 GB VRAM | local-node (32 GB GPU, 32,607 MiB) | Main local GPU row. |
| **H2** `gpu-128g` | Single GPU, 128 GB | datawatch-openai (B200, via OpenWebUI) | Large-model row. Only for models that do not fit H1 (gpt-oss:120b, qwen3-coder-next). |
| **H3** `gpu-undeclared` | GPU present but undeclared | `datawatch` (`hardware:{}`) | Cannot be classified. Probe first (S2); cells are recorded as `class_unknown` until then. |
| **H4** `cpu-only-24c` | **CPU-only, 24 physical cores / 48 threads, ≥ 64 GB RAM** | local-node's CPU, GPU hidden (`local-node-cpu` node, option (a)) | **First-class CPU row.** Distinct from H1 even on the same host. |
| **H5** `cpu-only-undeclared` | CPU-only, cores/RAM not declared | `datawatch` (no GPU declared; cores unknown) | Measured by the probe (S2). Recorded as `cpu_class_unknown` until measured. |

**Why H4 is separate from H5 and from "no declared GPU":** H4 has *measured* cores and RAM, so its advice can say "recommended on CPU-only, ≥ 24 physical cores". H5 may be a CPU-only box or a GPU box with undeclared hardware, and nothing is known yet. Folding them together would make the advice wrong on whichever side turns out not to match.

### Which combinations are meaningful

| Combination | Meaningful? | Reason |
|---|---|---|
| Claude × {low, medium, high} × `claude-code` × H0 | **Yes** | Core BL422 data. |
| Claude × effort × `goose`/`opencode` × any local H | **No** | Effort is not a parameter for these drivers. |
| Local model × `claude-code` | **No** | `claude-code` only runs Claude models. |
| Local model × {goose, opencode} × H1 | **Yes** | The main local-driver row. |
| Local model × {goose, opencode} × H4 (CPU) | **Yes, sentinel subset only** | Slow, so the cell subset is restricted (below). |
| Local model × `datawatch-default` × H1/H4 | **Yes** | Needed for the "default is wrong vs model is bad" finding (below). |
| Model ≥ 32B × H4 | **No** | Excluded by the CPU budget (see CPU discovery). |
| Model that fits H1 × H2 | **No** (only if H1 fails) | Wastes the B200 and gives no new information when H1 passes. Used only as a fallback when H1 cannot load the model. |
| Any model × `verdict` node | **No** | Excluded. Production verdict workload; shared GPU; no independent load. |
| Model × H3/H5 before the probe | **No** (recorded as `class_unknown`) | Advice must not be attached to an undeclared class. |

### Sweep shape (not a cross product)

- **Claude (H0):** only on a new Claude model id, or on a change to the BL422 default. Otherwise the BL422 results stand. Cells per full Claude sweep: 4 models × 3 efforts × 4 task types = 48 execution cells, plus 12 decompose cells (BL422 README: 13 decompose tiers including its default; this doc uses 12 for the 4 × 3 grid).
- **Local H1:** each candidate model × {goose, opencode} × the 4 task types, at fixed quantization. Up to 3 models → ≤ 24 cells.
- **Local H4 (CPU):** the same task types but **only the two lightest tasks** (`scripting`, `docs`), one candidate → ≤ 4 cells. Analysis and review are too slow on CPU to be informative inside the wall-clock bound.
- **`datawatch-default`:** one cell per hardware class that has a registered model, using the daemon's resolution with no override → ≤ 4 cells. This is the finding type below.
- **Repetition:** 2 reps per cell. A single rep is not enough to separate a model from noise.
- **Total per monthly run (no new Claude model):** ≤ 24 + 4 + 4 = **32 local cells**, within the 60-cell ceiling. A new Claude model adds ≤ 60 − 32 → must be run as a separate sweep to stay inside the ceiling.

### Grading (deterministic only)

- **Grading uses `binary_test` verifiers**, as BL422's `verify/<type>.sh` already do (`testdata/eval/model-tiering/README.md`). **BL259's `llm_rubric` cannot be used**, because it always fails (`internal/evals/evals.go:288-296`). Any LLM-judged rubric would need the BL259 "LLM grading" follow-up first (open question 7).
- The `review` task uses the fixed `candidate-pass.diff` / `candidate-fail.diff` pair with `expected.json`, which is also deterministic.

### Finding types — "datawatch default is wrong" vs "model is bad" (first class)

Every sweep result is classified as exactly one of these. They lead to different actions, so they must not be merged:

| Finding | Definition | Action |
|---|---|---|
| **`default_mismatch`** | The model passes on a hardware class, but the daemon's resolved default on that class does not select it (or selects a model that fails). | Propose a *default* change for that class. Does not mean the model is bad. |
| **`model_weak`** | The model fails across every backend and class it was run on, including its best-case cell. | Propose *excluding* the model from advice. Does not mean the default is wrong. |
| **`hardware_limit`** | The model passes on one class and fails on a lower one (e.g. passes on H1, fails on H4 by timeout). | Advice is conditioned on the class. Neither default nor model is at fault. |
| **`class_unknown`** | Cell ran on H3/H5 before the probe measured it. | No advice produced. Re-run after S2. |

---

## Scheduling mechanism

### Recommendation

**Use `schedule_spawn` with `cron_expr`, not `schedule_add`.** Concretely:

- `schedule_spawn` with `cron_expr: "0 3 1 * *"` (03:00 local, 1st of each month), `task` = a fixed prompt that calls `autonomous_template_instantiate` on the eval template (proposed name `recurring-model-eval`, created once with `autonomous_template_create`) and then reports back. Both MCP tools exist (`internal/mcp/server.go`, verified by the tool listing). Their parameter schemas were **not** checked in this pass; that is an S6 task.
- The ephemeral session does one thing: create the sweep PRD from the template. The sweep then runs through the existing autonomous executor, so it inherits the capacity ledger, BL428 caps, and BL431 Automation controls. This doc does not add a second executor.

### Why `schedule_add` is not enough

`schedule_add` with `cron_expr` (`internal/session/schedule.go:84-85`, `internal/mcp/server.go:1716`) sends text into an **existing session**. It needs a live, named session that is waiting for input. A monthly sweep should not depend on keeping a long-lived session alive for 30 days; a daemon restart or a session reset would silently skip the run. (`session_name` targeting, BL353, helps but does not remove that dependency.) `schedule_spawn` creates its own session on each fire.

### Does BL427 or BL431 already cover cadence?

**No, neither does.**

- **BL427** (`internal/autonomous/lifecycle_event.go`, Phase 1 shipped) is an in-process pub/sub on Automaton status events. It has no clock and no cron. Its Phases 2–5 add persisted subscriptions and webhooks, still event-driven. BL427 can notify on the sweep's *completion*, but it cannot fire the sweep.
- **BL431** (`automation-orchestration-design.md:899-930`) triggers on terminal PRD events. It advances a sequence of PRDs. It does not start one on a schedule.

**So:** `schedule_spawn` provides the cadence. BL427 can later *observe* the sweep's terminal state, and the operator can use that for the review notification. **No second scheduler is invented here.**

### Failure behavior

- If the spawn fires but the session fails to start: `schedule_spawn` already records `LastFireResult:"failed"` and `ConsecutiveFailures` (`schedule.go:97-100`). Surface that in the sweep status (S8).
- If the PRD does not reach a terminal state inside 8 h: recorded `timed_out`, not retried.
- A missed fire (daemon down at 03:00): not back-filled. The next monthly fire runs. This is noted in the results so a skipped month is visible.

---

## Results to defaults pipeline

### Where results are stored: a new record kind in the BL259 store, not a new store

- **Reuse the BL259 directory** (`~/.datawatch/evals/`, runs under `runs/`), so one place holds all eval output and existing `eval_list_runs` plumbing still works. Justification: BL259 already owns the persisted-run path and file layout (`evals.go:115-123, 212-222`).
- **Do not reuse the `Run` schema as-is.** `Run` has suite, mode, results, pass-rate, and threshold. It has no model, backend, effort, or hardware-class dimension, and that is the data the sweep exists to produce. Add a `kind: "sweep"` record with its own fields (`cell`, `hardware_class`, `driver`, `finding`).
- **Why not a new store:** a separate store would duplicate the run-listing and audit paths, and would mean two places to look for eval history.
- **Why not a DB table:** the `ArtifactData` database is for published pages; the daemon has no SQL store for evals. Using one here would add a dependency for no gain.

### Rollup to an operator-reviewable recommendation

1. **Per cell:** pass/fail from the verifier, plus wall-clock, tokens (Claude), and load time (local).
2. **Per (model, driver, hardware class):** pass rate over the reps, and a finding type from the table above.
3. **Per hardware class:** the recommended model list, ordered by pass rate then speed. Written as a **proposed advice set**, with a `status: "proposed"`, not applied.
4. **Diff against the current advice:** the sweep emits only the *changes* (new recommendation, removed recommendation, changed default-mismatch).
5. **Operator review:** the diff is shown in the PWA and CLI with the finding type, the cells behind it, and the raw run ids. Nothing takes effect until approved.

### Auto-apply vs. operator confirmation

**Recommended default: operator confirmation for every change. Nothing auto-applies in v1.**

Reasoning:

- The advice drives which model a real PRD runs on, so a wrong change costs real Claude spend or a wrong result. Both the BL428 and BL431 designs keep the operator in the loop for the same reason (BL428 "explicit config over inference", `2026-10-14-concurrency-and-budget-design.md:§3c`).
- The sweep's own measurements are noisy (2 reps per cell), and CPU rows are measured on one host.

**Optional bounded auto-apply (a later phase, off by default):** the sweep may auto-apply only **hardware-conditioned advisory entries** (the advice table used for display and suggestions), never the global default and never `llm`/`automation` model defaults. Each applied change stores the prior value so it can be reverted in one call, and it is logged to the audit trail. Off by default, switched on per hardware class. Open question 5.

---

## Hardware-conditioned advice shape

Replaces any single global "model X is recommended" with advice keyed on hardware class. Each entry has a `kind` discriminator, so a VRAM tier and a CPU tier can never be confused.

```json
{
  "schema": "eval-advice/v1",
  "model": "qwen3:8b",
  "driver": "goose",
  "task_types": ["scripting", "docs"],
  "advice": [
    {
      "hardware_class": { "kind": "gpu", "vram_gb_min": 24, "vram_gb_max": null },
      "recommendation": "recommended",
      "effort": null,
      "evidence": { "run_ids": ["<sweep-run-id>"], "pass_rate": 0.92, "reps": 2 },
      "finding": "hardware_limit"
    },
    {
      "hardware_class": { "kind": "gpu", "vram_gb_min": 0, "vram_gb_max": 16 },
      "recommendation": "not_recommended",
      "reason": "swept on 32 GB; no 16 GB class measured yet",
      "evidence": { "run_ids": [], "pass_rate": null, "reps": 0 },
      "finding": "class_unknown"
    }
  ]
}
```

**CPU-only entry, alongside the VRAM entries, same format, distinct `kind`:**

```json
{
  "model": "qwen3:1.7b",
  "driver": "goose",
  "task_types": ["scripting", "docs"],
  "advice": [
    {
      "hardware_class": {
        "kind": "cpu_only",
        "physical_cores_min": 24,
        "threads_min": 48,
        "ram_gb_min": 64,
        "gpu": "none_or_hidden"
      },
      "recommendation": "recommended_on_cpu_only",
      "reason": "measured on local-node-cpu (24C/48T, GPU hidden); 1.7B fits the budget",
      "evidence": { "run_ids": ["<sweep-run-id>"], "pass_rate": 0.75, "reps": 2 },
      "finding": "hardware_limit"
    },
    {
      "hardware_class": { "kind": "cpu_only", "physical_cores_min": 0, "threads_min": 0, "ram_gb_min": 0, "gpu": "none_or_hidden" },
      "recommendation": "not_recommended",
      "reason": "CPU class not measured; H5 undeclared",
      "evidence": { "run_ids": [], "pass_rate": null, "reps": 0 },
      "finding": "cpu_class_unknown"
    }
  ]
}
```

Rules for the shape:

- `kind` is one of `gpu`, `cpu_only`, `hosted`. A `cpu_only` match never falls back to a `gpu` entry, and the reverse also holds. "No declared GPU" is **not** a `cpu_only` match; it is `class_unknown` until probed.
- Recommendations are per `(model, driver, hardware_class)`. A model with no entry for a class has no advice for it.
- `recommendation` values: `recommended`, `recommended_on_cpu_only` (CPU row only), `not_recommended`, `unknown`.
- `evidence` must reference real sweep run ids. Advice without evidence is rejected at write time.
- Advice stays `proposed` until approved; the consumer reads only approved entries.

---

## Cost and capacity bound for the sweep

### What BL428 sets (cited)

- **§3b** (`concurrency-and-budget-design.md:321-360`): `llm:<name>` `max_inflight` is the per-backend ceiling, currently `0` = unlimited for `claude-code`. Live `capacity_status` confirms `llm:claude-code limit 0`. §3b also says local `node:<name>` limits are the hardware ceiling.
- **§4d** (`:514-632`): `plan_tier` sets `max_inflight` when unset, and the conservative default is `1`. The tier→number table is operator policy, not published by Anthropic.
- **§4e** (`:633-689`): a shared `billing_group` makes two daemons share one `backoff:` flag. If `ralfthewise` (the federation peer) shares this Claude account, its usage counts against this sweep's window.

### What this sweep does

- **Respects §3b for Claude:** the sweep's Claude cells run with `llm:claude-code` capped at **1** for the sweep's duration, through a dedicated capacity lease. This is a per-run cap, not a change to the global default, so it is not a BL428 override.
- **Respects §3b for local:** `node:local-node-cpu` and `node:local-node` each get a limit of **1** during the sweep, so no two sweep cells run at once on one node. The sweep does not share a node with a live GPU workload it did not start.
- **Respects §4e:** before the first Claude cell, the sweep checks `backoff:<billing_group>`. A set flag aborts the Claude portion and records `skipped_backoff`. The local portion still runs.
- **Deliberately overrides nothing permanent.** The sweep does not change `max_inflight`, `plan_tier`, or any global default. The per-run cap is released when the sweep ends, including on failure (lease released in a `finally`-style path).

### Caps on the sweep itself

| Cap | Value | Source |
|---|---|---|
| Wall clock | 8 h | Discovery-loop bound |
| Cells | ≤ 60 (typical 32 local + ≤ 24 Claude when a new Claude model exists) | Test-matrix bound |
| Claude concurrency | 1 | §3b, sweep-scoped |
| Local concurrency per node | 1 | §3b `node:` pool |
| Claude dollars | Operator-set; proposed default $25 per run | Proposal; open question 3 |
| Retries | 0 for timed-out cells; 1 retry for infrastructure errors (not for verifier failures) | Keeps results honest |

---

## Parity surface

Per AGENT.md's parity rule (`AGENT.md:259`, item 8), each surface is included or excluded with a reason. The three required operations are: **trigger an on-demand run**, **view the latest monthly results**, and **review and approve a proposed default change**.

| Surface | Decision | Reason / what it covers |
|---|---|---|
| **REST** | **Include** | `POST /api/evals/sweeps` (trigger), `GET /api/evals/sweeps/latest` (view), `GET /api/evals/sweeps/<id>` (detail), `POST /api/evals/sweeps/<id>/proposals/<pid>/approve` (approve). Matches the existing `/api/evals/*` pattern (`server.go:354-358`). |
| **MCP** | **Include** | `eval_sweep_run`, `eval_sweep_latest`, `eval_sweep_approve`, alongside the existing `eval_run` / `eval_list_runs` (`internal/mcp/evals.go:19-37`). Approval needs an explicit `confirm:true` argument. |
| **CLI** | **Include** | `datawatch eval sweep run | latest | approve`. Command names proposed; not verified against the current CLI tree in this pass. |
| **Comm channel** | **Include, view and trigger only** | Operator can request the latest summary and trigger a run from chat. **Approval is excluded from comm channels**: it changes a default that drives real PRDs, and a chat message is too easy to send by mistake. Approval happens in PWA, CLI, or REST with confirmation. |
| **YAML / config** | **Include** | New `eval.sweep` block: `schedule` (cron), `candidates` (operator list), `wall_clock_hours`, `max_cells`, `claude_dollar_cap`, `cpu_node` name, `auto_apply` (default `false`). All keys are new; no existing default changes. |
| **PWA** | **Include** | A results page showing the latest sweep, the proposed diff with finding types, and an approve control. Client-visible, so the Mobile-Parity rule applies on implementation. |
| **Android** | **Include as proposal only** | Same read-and-approve capability as the PWA (AGENT.md Mobile-Parity rule, capability parity required, implementation parity not). **No mobile changes in this pass.** On implementation, file a datawatch-app issue per the Mobile-Parity rule. |
| **iPhone / iOS** | **Include as proposal only** | Same as Android. No change in this pass. |

---

## Follow-up implementation PRD section

Staged. Each stage is independently reviewable. None of this is implemented here.

**S1 — Data model and store.** Add the `kind:"sweep"` record and the `eval-advice/v1` shape to the BL259 store. Acceptance: round-trip test; old `Run` records still list correctly.

**S2 — Hardware probe.** Add probing for each compute node: GPU name and VRAM (from the node, via its own Ollama/OpenWebUI endpoint or a probe command), and CPU physical cores, threads, and RAM. Store measured values next to the declared ones, and show both. Acceptance: `local-node` reports 24C/48T and 32,607 MiB; `datawatch` reports unknown rather than assuming. Also resolves the 64-core discrepancy in the record.

**S3 — CPU-only compute node.** Register `local-node-cpu` (second Ollama server with GPU hidden, option (a)). This step is an operator action on the host; the PRD task documents the exact commands and does not run them without confirmation. Acceptance: `compute_node_list` shows the node with measured CPU fields and `gpus:0`.

**S4 — Discovery loop.** Catalog diff, operator candidate list, registered-unevaluated diff. Enforces the candidate bounds (≤ 2 new + ≤ 1 CPU). Pulls require confirmation. Acceptance: a dry-run lists candidates and bounds without pulling.

**S5 — Matrix runner.** Implements the sweep shape (cells, reps, sentinel CPU subset, `verify/*.sh` grading). Excludes verdict node. Acceptance: a dry-run produces the cell list and counts; a 1-model run completes.

> **Status: In Progress (BL449, 2026-10-15).** The first slice, a fixed 16-cell open-model matrix (4 opencode roles × 4 task types) with the three-verdict split, shipped as a driver and fixtures: `scripts/eval_open_model_matrix.py`, `testdata/eval/open-model-matrix/`. Daemon fixes found while running it shipped in **v9.1.3** (opencode ask adapter strips the `ollama/` prefix) and **v9.1.4** (session start normalises the opencode model for Ollama nodes). There is no valid run yet: r6 was `invalid-infrastructure`, and r8 (on v9.1.4) was invalid for scoring because fast/mid workers still die in 3–7 s and session records were evicted before collect. Reps, the sentinel CPU subset and the dry-run acceptance are not done. Plan: [2026-10-15-bl449-open-model-matrix.md](2026-10-15-bl449-open-model-matrix.md).

**S6 — Scheduler and template.** Create the `recurring-model-eval` autonomous template; wire `schedule_spawn` with the monthly cron. Verify `autonomous_template_*` parameter schemas first (not checked in this pass). Acceptance: a manually fired spawn instantiates the template PRD.

**S7 — Rollup and advice.** Finding classification (`default_mismatch`, `model_weak`, `hardware_limit`, `class_unknown`), diff against current advice, proposed advice written with evidence. Acceptance: a sweep with a known-bad model yields `model_weak`, not `default_mismatch`.

**S8 — Approval and status.** REST, MCP, CLI, PWA approval flow with `confirm:true`; sweep status and `ConsecutiveFailures` surfaced. Acceptance: a proposal cannot take effect without approval; a failed spawn is visible.

**S9 — Caps and backoff.** Sweep-scoped `llm:claude-code` cap, `node:` limits, §4e backoff check, dollar cap via `cost_usage`. Acceptance: a set `backoff:` flag aborts the Claude portion only.

**S10 — Parity and docs.** Parity surfaces per the table; `docs/` updates per AGENT.md's Documentation Rules; mobile issues filed for Android and iOS. Acceptance: parity checklist complete.

**Phase order:** S1 → S2 → S3 (operator) → S4 → S5 → S6 → S7 → S8 → S9 → S10. S2 must precede S5, so no H3/H5 cell is ever reported as a real class.

---

## Open questions

1. **Core count for local-node.** The operator stated 64 cores; `lscpu` on this host reports 24 physical / 48 logical. Is the 64-core figure for a different machine, or is it a mistake? This doc uses the measured figures.
2. **CPU-only node: separate node (option a) or flag on the existing node?** This doc recommends a separate `local-node-cpu` node so the GPU server is never touched during a CPU run.
3. **Dollar cap per sweep.** Proposed $25 per run. This is an assumption. The real cost of one full Claude sweep should be measured from `rates.json` and `cost_usage` before the cap is set.
4. **Which models are the first candidates?** Proposed: `qwen2.5-coder:7b` (catalog, not yet registered) and one CPU candidate. Operator to choose.
5. **Auto-apply.** Recommended off. Should a later phase allow it for advisory entries only, per hardware class? (The proposal above says yes, later, off by default.)
6. **Live catalog.** Should the daemon scrape the Ollama library for discovery (the `marketplace.go:161-163` future work), or stay curated? Curated is the default here.
7. **LLM-judged grading.** BL259's `llm_rubric` always fails. Should the BL259 "LLM grading" follow-up be prioritized, or are deterministic verifiers enough for the sweep? This doc assumes deterministic.
8. **Where does the sweep config live?** Proposed `eval.sweep` in YAML. Confirm the YAML key placement against the Configuration Accessibility rule.
9. **Verdict node exclusion.** Confirm that `verdict` should be permanently excluded, or whether a read-only observation of its load is acceptable.
10. **`ollama_stats` is unavailable on this server (404).** Is it expected to be served, or should the probe in S2 use another route?
11. **Effort for local models.** The effort axis is Claude-only. Should local models get a comparable second axis (quantization, context length) in v1, or stay fixed?
12. **Federation peer.** Does `ralfthewise` share the Claude account? If yes, §4e's `billing_group` must be set before the first sweep.
13. **Autonomous template parameter schemas.** `autonomous_template_create` / `_instantiate` exist as MCP tools, but their argument schemas were not checked in this pass. Verify before S6.
14. **Verification tier.** BL422 excludes the verifier from fixtures because there is no per-task override. Should the sweep test the verifier via a guarded global flip, as BL422 does, or leave verification out of the recurring sweep?

---

## Checklist

- [x] Parity surface section present, with each surface included or excluded and a reason
- [x] Status line: needs_review, 2026-10-14, BL437
- [x] No version bump
- [x] No implementation, no default changes, no mobile changes, no model pulls
- [x] Live data cited from read-only calls; unverified points marked

Verified complete on 2026-10-14, no changes needed.
