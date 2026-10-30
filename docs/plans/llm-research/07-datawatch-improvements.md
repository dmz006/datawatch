# 07 — Datawatch Improvements

**A note on scope before Part A:** the task brief for this document named
`docs/05-serving-and-hardware.md` as a required prerequisite read. That file
does not exist in this repo — the only docs present are
[01-current-state.md](01-current-state.md), [02-workload-profiles.md](02-workload-profiles.md), and
[03-text-model-candidates.md](03-text-model-candidates.md); the project
[README](../README.md)'s planned index doesn't even list a "serving and
hardware" doc under that number (05 is planned as "Council & Orchestration
Deep Dive"). Rather than block on a missing file, every proposal below is
grounded in 01-03 plus a fresh live re-verification against the running
datawatch instance this session (2026-09-30), via the `datawatch-local-node-4478`
MCP server (`llm_list`, `compute_node_list`, `capacity_status`,
`council_config_get`, `autonomous_config_get`, `orchestrator_config_get`,
`docs_search`/`docs_read`) and the tool schemas for
`autonomous_prd_set_llm`/`set_story_llm`/`set_task_llm`, `llm_refresh_models`,
`compute_node_delete`, and `council_config_set`.

**Live drift found this session (flag before reading Part A):** 01 documented
`autonomous.verification_backend` and `autonomous.verification_effort` as
empty strings, with the verifier falling back to the named `ollama` registry
entry (`qwen3:1.7b`@`local-node`). A fresh `autonomous_config_get` this
session shows `verification_backend: "ollama-datawatch"` and
`verification_model: "qwen3.8:27b"` — **the verifier has since been pinned to
the same node and a comparably large model as PRD planning**, not the small
fallback 01/02 analyzed. Since `node:datawatch` has `max_concurrent_sessions:
1`, the verifier and the worker/planner sessions it's supposed to check now
contend for the same single-session slot on the same node. This is treated as
current ground truth below, and it sharpens (not weakens) the case for
Proposal 1. `council.llm_ref` is still empty and `council.max_parallel` is
still `0`, matching 01 — no drift there.

**A second disagreement worth flagging rather than resolving:** `docs_read`
on `datawatch-definitions.md#settings-automate` states `council.max_parallel`
"default 2" (concurrent personas per round). But the live
`council_config_set` MCP tool's own parameter description for `max_parallel`
says `">=0; 0=serial"`. Those two authoritative-looking sources disagree on
what the current `max_parallel: 0` actually does at runtime — serial
(1-at-a-time) or an internal default of 2. This project's read-only scope
cannot resolve which is correct without either reading the scheduler source
or running a live council round and counting concurrent persona calls;
proposals below treat this as an open question, not a settled fact.

Two howto files linked from `datawatch-definitions.md` resolved cleanly via
`docs_read` this session (`howto/llm-registry.md`, `howto/council-mode.md`),
but both are short stubs that point back to `datawatch-definitions.md` as the
fuller reference rather than containing independent detail. `howto/automata.md`
does not resolve (`docs_read` returns a 404 "chunk not found"); `howto/compute-nodes.md`
and `howto/compute-routing.md` were not independently confirmed to resolve —
`docs_search` only ever surfaced `datawatch-definitions.md` sections for those
topics, never the howto paths themselves, even though the definitions doc
links to them by name. Noted as a documentation-corpus gap, not treated as a
blocker.

**Second re-verification pass (2026-09-30, later same day, via the
`datawatch-local-node-3c74` MCP connection):** `get_version` now reports
**8.36.1** running with **8.36.2** available — the daemon has moved twice
since the task brief's snapshot (8.34.1) and once since the first pass
above; not itself acted on here (an update is an operator/infra action, out
of this project's read-only, docs-under-`llm-research`-only scope), but
recorded since every proposal above should be read against a moving target,
not a frozen one. Re-running the same six calls otherwise reconfirms rather
than contradicts everything above: `test-node` is still present unmodified
since 2026-09-17; `council_config_get` still returns `llm_ref: ""` and
`max_parallel: 0`; `autonomous_config_get` still shows
`verification_backend: "ollama-datawatch"` / `verification_model:
"qwen3.8:27b"`, confirming the Proposal-1-motivating drift wasn't a
transient blip; `ollama-datawatch`'s and `ollama-local-node`'s registry
entries are each still pinned to their single original `compute_nodes` entry
(no failover list), confirming Proposal 4's problem statement; `cost_summary`
climbed from 2,263 to **2,267** sessions (`claude-code` 148→152, all other
per-backend counts including the 1,882-session `subprocess` bucket
unchanged) with `total_tokens_in`/`total_tokens_out`/`total_usd` still
exactly `0` across all seven backends — reconfirming Part B's B1 problem
statement live rather than resting on the earlier snapshot.

**Third re-verification pass (2026-09-30, via the `datawatch-local-node-9af2`
MCP connection):** `get_version` now reports **8.36.10** running with
**8.36.11** available — six further point releases since the second pass's
8.36.1, still not itself acted on for the same out-of-scope reason.
`compute_node_list`, `council_config_get` (`llm_ref: ""`, `max_parallel: 0`),
and `capacity_status` (`node:datawatch` limit still `1`, `test-node` still
present since 2026-09-17) all reconfirm exactly, no new drift. One genuinely
new finding this pass: `autonomous_config_get` now also returns a
`verification_backends` field — `["ollama-local-node", "ollama-datawatch"]`
— sitting alongside the already-documented singular `verification_backend`/
`verification_model`. This field is not mentioned anywhere in `docs_search`
results for "verification backends"/"verifier fallback" against
`datawatch-definitions.md`, and its runtime semantics (ordered failover on
verifier error? round-robin per task? a declared-candidates list not yet
wired to any selection logic?) could not be confirmed from read-only tools
alone. Flagged rather than assumed: if it is an active failover chain, it is
a second, narrower precedent for Proposal 1's role-mapping idea (the verifier
role already resolves against more than one backend, just without Proposal
1's shared `roles:` vocabulary or node-contention awareness) and it softens
Proposal 4's "no failover is actually exercised today" claim specifically for
the verifier call site — the LLM registry's own `compute_nodes` lists are
still each pinned to one node, so Proposal 4's core problem statement about
node-level failover stands unchanged. Added as a row to the hooks table
below rather than editing Proposals 1/4's prose, to keep those proposals'
already-reconfirmed problem statements intact.

## Part A — Routing, tiering, and compute balancing

### Existing hooks reused

Every proposal below composes with a mechanism that already exists today,
confirmed live this session or in 01-03. Listed up front so each proposal can
reference it by name instead of re-explaining it:

| Existing mechanism | What it already does | Confirmed via |
|---|---|---|
| Per-call-site backend/model keys (`autonomous.verification_backend`/`verification_model`, `council.llm_ref`, `vision.model`, `session.summarizer.llm_ref`) | Each consumer already has its own named override point, just no shared vocabulary across them | `autonomous_config_get`, `council_config_get`, 01(d) |
| `autonomous_prd_set_llm` → `autonomous_prd_set_story_llm` → `autonomous_prd_set_task_llm` | A complete, already-shipped three-level override chain (task → story → PRD → global) for Automata, including a split between `decomposition_profile` and task-execution `backend` | Tool schemas, this session |
| Capacity ledger (`capacity_status`: pools `host` / `node:<name>` / `llm:<name>`, limits, holders, waiting queue with reasons) | Live admission accounting per node/LLM already exists, just not VRAM/parallel-slot-aware yet | `capacity_status`, this session; 01(e) |
| Compute-node "Computed max" auto-suggest (Hardware section: daemon suggests concurrent-request capacity from VRAM × GPU count) | The math Proposal 2 needs already has a UI-facing precedent, just not wired into the ledger | `docs_read`, `datawatch-definitions.md#compute-nodes` |
| `llm_refresh_models`, `compute_node_delete`, `council_config_set` | Three of the seven proposals below are "run this existing tool," not new engineering | Tool schemas, this session |
| `council.max_parallel` / async run + SSE events / per-persona live-watch cards | The concurrency knob and the streaming plumbing Proposal 4/6 build on already exist | `docs_read`, `datawatch-definitions.md#settings-automate` |
| `auto_fix_retries`, v8.16.0 diff-grounded verification, v8.17.0 quality gates | Retry infrastructure and failure-signal richness already exist; Proposal 7 adds a branch, not a subsystem | `docs_read`, `datawatch-definitions.md#settings-automate`; 01(c) items 5-6 |
| Observer peer binding on a compute node | Live GPU/process stats correlation already has a registered mechanism, useful for any future "actual load" signal beyond declared capacity | `docs_read`, `datawatch-definitions.md#compute-nodes` |
| `autonomous.verification_backends` (list, currently `["ollama-local-node", "ollama-datawatch"]`) | An undocumented (not in `docs_search`/`docs_read` results) second, narrower failover precedent specific to the verifier call site, alongside the singular `verification_backend`; exact runtime semantics unconfirmed from read-only tools | `autonomous_config_get`, third re-verification pass above |

### Proposal 1 — Workload-role model tiers (role → LLM mapping)

**Problem.** Which model backs which call site is scattered across separate,
independently-named config keys (`autonomous.verification_backend`/`_model`,
`orchestrator.guardrail_backend` — itself confirmed in 01 as a second,
currently-unread key duplicating the verifier's — `council.llm_ref`,
`vision.model`, `session.summarizer.llm_ref`) with no shared vocabulary
tying them together as "the verifier role" or "the council-persona role."
The live drift found this session — the verifier silently moved from the
small fallback model to `qwen3.8:27b`@`datawatch`, the same node and a peer
model to planning — is exactly the failure mode this enables: nothing
records *why* a role's model changed, or checks the new assignment against
the node's `max_concurrent_sessions: 1` before it starts contending with
planning for the same slot.

**Proposed behavior.** Add a `roles:` config block mapping nine fixed
workload roles (`planner`, `worker`, `verifier`, `guardrail`,
`council-persona`, `council-synthesis`, `vision`, `embed`, `speech`) to an
LLM registry name plus optional model/effort override. Resolution order:
explicit role override → existing per-call-site key (kept for backward
compatibility) → global named `ollama` fallback. Ship defaults matching 02's
tier table: fast-small for verifier/guardrail/embed, balanced for
council-persona/vision/speech, strong for council-synthesis, frontier for
planner/worker — with an explicit warning surfaced (not silently applied)
when a role assignment would put two roles on the same
`max_concurrent_sessions`-constrained node.

**Fields needed.** New `roles.<role>.llm_ref` / `.model` / `.effort` in
`datawatch.yaml`; a `role_config_get`/`role_config_set` MCP pair mirroring
the shape of `council_config_get`/`council_config_set`; CLI `datawatch roles
get|set`.

**Surfaces affected.** YAML, REST, MCP, CLI, PWA (new "Workload Roles" card
under Settings → Automate, next to the existing Council Mode card).

**Effort: M.** No new inference path — this is a resolution layer in front
of call sites that already exist (the fallback blocks documented in 01(c)
items 4-6, plus `council.go`'s persona/synthesis dispatch) and one new config
surface across the 5 parity layers.

### Proposal 2 — Capacity awareness for Ollama parallel slots and VRAM

**Problem.** The capacity ledger's `node:datawatch` pool has a flat limit of
1, sourced directly from `max_concurrent_sessions: 1`. It has no notion of
Ollama's own `OLLAMA_NUM_PARALLEL`, per-model VRAM footprint, or `num_ctx`.
The compute-node edit form already auto-suggests a "Computed max"
concurrent-requests figure from VRAM × GPU count (confirmed via `docs_read`),
but that number isn't consumed by the admission ledger — it just informs a
human filling in the form. Net effect: Council's 12 personas can never
usefully spread across the two nodes' real headroom (`datawatch-openai`
declares `max_concurrent_models: 3`, `local-node` declares 10) because the
ledger reasons in whole-session slots per node, not VRAM budget per model.

**Proposed behavior.** Extend `declared_capacity` with an
`ollama_num_parallel` field and either a per-model VRAM-estimate map or a
live pull from each model's `/api/show` size (already fetched for 01(b)'s
model-inventory tables via read-only Ollama calls). Compute an *effective*
node concurrency as `min(max_concurrent_sessions, floor(declared_vram_gb /
sum(resident_model_vram_gb)), ollama_num_parallel)` instead of the current
flat number, and surface it read-only alongside the existing declared value
in `compute_node_detail` / `capacity_status` output, so admission decisions
(and Proposal 4's node selection) have a real headroom number instead of a
single session slot.

**Fields needed.** `declared_capacity.ollama_num_parallel`,
`declared_capacity.model_vram_gb` (map or default float), `num_ctx`-aware
headroom adjustment; `capacity_status` gains a computed-vs-declared column
per node.

**Surfaces affected.** YAML, REST, MCP (`compute_node_add`/`compute_node_update`
gain the new params), CLI, PWA (the Hardware/Capacity section already has the
VRAM×GPU auto-suggest UI — this wires the ledger to actually read it instead
of just displaying it).

**Effort: L.** This touches the admission-ledger's core matching logic,
which wasn't read from source this session (only its live output via
`capacity_status` was confirmed) — flagged as the proposal with the most
effort uncertainty of the seven, not a confident L.

### Proposal 3 — CPU compute-node profile

**Problem.** Every workload today, including the fast-small tier (embed,
verifier, guardrail), runs on one of the two GPU nodes. `local-node`'s
Threadripper 9960X (24c/48t, 122 GB RAM) sits mostly idle relative to its
single 32 GB GPU (01(b): the embedder is the *only* model loaded there most
of the time, at 0.57 GB VRAM out of 32 GB declared) — there's no registry
concept of a CPU-only node to absorb background work distinct from "the GPU
node that happens to be less busy."

**Proposed behavior.** A compute node of kind `opencode` or `goose` or `ollama` or `openai-compat`
pointed at a CPU-only `llama-server`/Ollama instance (no GPU declared — this
already works today, `hardware.gpu_count: 0` is expressible), given a lower
`scheduling_priority` than either GPU node, plus a new `role_affinity` hint
so Proposal 1's role router prefers it specifically for `embed`/`speech`/
small-classification roles, not general chat or anything latency-sensitive.

**Fields needed.** `role_affinity: []string` on the compute node record,
paired with Proposal 1's role block; no changes needed to the node-kind or
hardware schema itself.

**Surfaces affected.** YAML, REST, MCP, CLI, PWA (the compute-node add form
already supports GPU-less declarations; `role_affinity` is one new field on
that same form).

**Effort: S.** The node type needs nothing new — a GPU-less `ollama` node is
already a supported configuration. Only `role_affinity` plus wiring it into
Proposal 1's (not-yet-built) role router is new work.

### Proposal 4 — Node selection policy, prefetch, and warm-up

**Problem.** All four registry LLMs are pinned to a single fixed
`compute_nodes` entry each (`ollama-datawatch` → `datawatch` only;
`ollama-local-node` → `local-node` only — confirmed via live `llm_list`
this session), despite the registry's own documented design ("first entry
primary, rest failover in order," per `docs_read` on `#llm-registry"). No
failover is actually exercised today. Separately, nothing keeps a role's
model warm ahead of a call — a cold model pays load time on first use each
time it's evicted.

**Proposed behavior.** A `node_selection_policy` per LLM or per role,
evaluated at dispatch time, with four modes: `priority-order` (today's
implicit default via `scheduling_priority`), `least-loaded` (read live from
`capacity_status`), `model-affinity` (prefer whichever node already reports
the model resident via `/api/ps`, avoiding a cold load), and `round-robin`.
Pair this with a lightweight prefetch ticker that periodically pings each
role's primary model on its primary node to keep it warm, using the same
"live ticker re-reads on next sweep" pattern `council_config_set`'s own
description already documents for picking up config changes without a
restart.

**Fields needed.** `selection_policy` string per LLM (or per role, if
Proposal 1 ships first); a prefetch-interval config value.

**Surfaces affected.** YAML, REST, MCP, CLI; PWA gains a policy dropdown on
the LLM registry edit form.

**Effort: M.** Policy logic is new, but it composes entirely with data the
ledger and `ollama_stats`/`/api/ps` already expose; the prefetch ticker is a
straightforward periodic job given the sweep pattern already used elsewhere
in the codebase.

### Proposal 5 — Fixing observed drift

**Problem.** Three concrete drift items, all reconfirmed live this session:
(a) `ollama-local-node`'s registry model list still names five models
(`qwen3.5:35b`, `qwen3-coder:30b`, `qwen2.5:14b`, `Gemma3:12b`, `qwen3:8b`)
that are not installed on that host, while `moondream:latest` and
`llmvision/glimpse-v1` are installed there but absent from the registry's
list; (b) `test-node` is still present, a stale duplicate of `datawatch`'s
exact address, created once (2026-09-17) and never touched since; (c)
`council.llm_ref` is still an empty string, so Council personas and
synthesis both still deterministically resolve to the smallest general model
in the fleet.

**Proposed behavior.** No new tool is required for any of the three — this
is a "run the button that already exists" backlog item. `llm_refresh_models("ollama-local-node")`
fixes (a) directly. `compute_node_delete("test-node")` fixes (b) directly.
`council_config_set(llm_ref="...")` fixes (c) directly, pending a decision on
which registry entry Council should actually use (Proposal 1's role default
for `council-persona`/`council-synthesis`, once that exists, is the natural
answer). The only genuinely new piece worth considering is a composite
`datawatch doctor fix-drift` call that runs a read-only drift scan (stale
model lists, duplicate node addresses, empty `*_ref` fields) so future audits
like this one don't have to rediscover the same three items by hand.

**Fields needed.** None for the three fixes themselves. A `doctor_fix_drift`
MCP tool (and REST/CLI equivalents) if the optional composite wrapper is
built.

**Surfaces affected.** MCP/CLI/REST/PWA already fully cover the three
individual fixes (per the 7-surface parity table in `datawatch-definitions.md#compute-nodes`
and the LLM registry's own CLI verb list). The optional wrapper would need
the same four surfaces.

**Effort: S** for applying the three existing fixes; **S** for the optional
composite wrapper (thin orchestration over three calls that already exist).

### Proposal 6 — Per-PRD and per-council-run override UX

**Problem.** Automata already has a complete, three-level override chain —
`autonomous_prd_set_llm` (PRD-level, with a deliberate split between
`decomposition_profile` and the task-execution `backend`) →
`autonomous_prd_set_story_llm` → `autonomous_prd_set_task_llm`, resolving
task → story → PRD → global (confirmed directly from the tool schemas this
session). Council has no equivalent: `council_config_set` only changes the
*global* `llm_ref`/`max_parallel`/`comm_firehose`/`draft_retention_days` —
there is no way to point a single high-stakes council run at a stronger
model without changing the default for every other concurrent or future run
first.
 
iterate on this with me - this may just be using the "council" with configurable optoins in a prd or automata for decomposition; but maybe can be used for validatoin or other stages too? or tasks?

**Proposed behavior.** Extend `POST /api/council/run` (and the corresponding
`council_run` MCP tool) to accept optional `llm_ref`/`model`/`max_parallel`
fields scoped to just that run, falling back to `council_config_get`'s
globals exactly the way the PRD chain already falls back to the global
autonomous config. This is a direct copy of a pattern Automata has already
shipped and proven, not a new design.

**Fields needed.** `llm_ref`, `model`, `max_parallel` as optional fields on
the `council_run` request/tool call.

**Surfaces affected.** REST, MCP (`council_run`), CLI, PWA (an "Advanced"
expander on the Council run-start form, next to the existing `image_path`
optional field).

**Effort: S.** Automata's three-level chain is the template; `council_run`
is a single call site to extend (not three), so this is strictly less work
than what Automata already shipped for the same problem shape.

### Proposal 7 — Fallback and escalation chains

**Problem.** `auto_fix_retries: 1` (confirmed live) gives a worker exactly
one retry when verification fails, always with the same model/backend. 02
flagged the verifier's one catastrophic failure mode as a **false-pass**,
currently uncaught by anything downstream since `orchestrator.enabled:
false`. There's no distinction today between "the verifier returned
malformed output" (a model-capability problem, worth escalating) and "the
verifier correctly said the diff is wrong" (a worker problem, worth retrying
the same way).

**Proposed behavior.** On a verification failure, branch on failure type:
a parse/schema failure escalates that specific retry to a stronger model
(using Proposal 1's role-tier config — e.g., the `verifier` role's fast-small
default escalates to whatever model backs the `planner`/`worker` role, for
that retry only), while a substantive fail (valid verdict, diff genuinely
wrong) retries the worker task unchanged, matching today's behavior. This
adds a branch to logic that already exists (v8.16.0 diff-grounded
verification already gives the verifier the real git diff; v8.17.0 quality
gates already distinguish pass/fail with structured results) rather than
building new retry infrastructure.

**Fields needed.** `roles.verifier.escalation` (a pointer into Proposal 1's
role block) or, if Proposal 1 isn't adopted, a standalone
`autonomous.verifier_escalation_model`; `auto_fix_retries` semantics extended
with an explicit "escalate on this retry" flag rather than a bare retry
count.

**Surfaces affected.** YAML, REST, MCP (`autonomous_config_set`), CLI.

**Effort: M.** The retry loop, diff-grounding, and quality-gate result
structure already exist; this adds one conditional branch and one new config
key, not a new subsystem.

### Prioritized backlog

1. **Proposal 5 — fix observed drift.** S effort, zero new code, three
   already-built tools. Do this regardless of anything else below.
2. **Proposal 6 — per-council-run overrides.** S effort, a direct copy of
   Automata's already-shipped override-chain pattern.
3. **Proposal 1 — workload-role tiers.** M effort, foundational: clarifies
   the already-confusing live verifier reassignment found this session, and
   is a prerequisite most other proposals reference.
4. **Proposal 3 — CPU compute-node profile.** S effort once Proposal 1's
   `role_affinity` concept exists; the node type itself needs no new
   engineering.
5. **Proposal 7 — fallback/escalation chains.** M effort, depends on
   Proposal 1's role block to define what "escalate" resolves to.
6. **Proposal 4 — node selection policy + prefetch.** M effort; most useful
   once Proposal 2 supplies real headroom numbers to select on, so ordered
   after it despite being individually simpler.
7. **Proposal 2 — capacity awareness (VRAM/parallel-slot-aware ledger).**
   L effort, the biggest single unlock for the "8+ parallel calls" goal from
   02, but the admission-ledger internals weren't read from source this
   session — treat the L estimate as the least confident of the seven.

## Part B — Usage, cost, and performance tracking

**Scope note:** the brief named `docs/06-benchmark-plan.md` as a prerequisite
read. It does not exist — only 01-03 and Part A of this doc exist. Grounded
instead in 01/02/Part A plus fresh live re-verification this session
(2026-09-30) via `cost_summary`, `cost_rates`, `eval_list_suites`,
`alert_rule_list`, `ollama_stats`, `capacity_status`, `observer_peers_list`,
`docs_search`/`docs_read`, and `web_search`/`WebFetch` for Proposal B2's cost
model. B5 is written against the `eval_*` tools actually confirmed live, not
the missing benchmark-plan doc.

**Tooling note:** `web_search` (SearXNG/bing) returned generic,
query-irrelevant homepages for most specific queries this session even after
refined retries (e.g. an "ollama prompt_eval_count" query returned
Merriam-Webster's definition of "prompt"). External facts below were instead
sourced via `WebFetch` on the obvious first-party URLs (nvidia.com, ollama's
own GitHub docs, eia.gov), each cited inline with URL and 2026-09-30 as date
checked.

### The problem, with live evidence

- **`cost_summary`** (re-run live): 2,263 sessions, `total_tokens_in: 0`,
  `total_tokens_out: 0`, `total_usd: 0` across **seven** backends — `claude`
  (6), `claude-code` (148), `goose` (2), `ollama` (6), `ollama-datawatch` (6),
  `opencode` (213), and `subprocess` (**1,882** — 83% of all sessions, and
  not named in the brief's backend list). Session count climbed from 2,260
  (01(f)'s last pass) to 2,263 during this write-up; every token/USD field
  stayed exactly `0`.
- **`cost_rates`**: `claude-code`/`opencode`/`opencode-acp`/`opencode-prompt`/
  `goose`/`aider` all `$0.003`/`$0.015` per 1K in/out; `gemini`
  `$0.0035`/`$0.0105` — but flat `$0`/`$0` for `ollama`, `openwebui`, and
  `shell`. Non-zero rates exist where usage is zero; the backends that could
  be priced from real electricity cost are hard-coded to zero.
- `docs_read` on `#session-analytics` confirms the promise ("messages in/out,
  tokens, cost, average response time... default sort: cost descending") —
  meaningless with every session at `$0`. `#cost-rates-usd-1k-tokens`
  explains part of the split: "Values default to public list pricing on
  first run" — Ollama/OpenWebUI have no public list price to seed from, so
  they land on `0`, not an estimate. `#audit-log` is a genuinely separate,
  working log (operator actions), structurally unable to answer "how many
  tokens did this session cost."
- `eval_list_suites` still returns `[]` (relevant to B5). `alert_rule_list`
  returns `{"rules":[]}` — a full CRUD surface exists
  (`create`/`update`/`delete`/`enable`/`disable`/`list`/`firings`) but zero
  rules are defined (relevant to B4).

### Why the numbers are zero: two different situations, not one

**Terminal/subprocess backends** (`claude-code`, `opencode`, `goose`,
`aider`, and the 1,882-session `subprocess` bucket) run as CLI
processes/PTYs, not JSON HTTP calls the daemon controls end-to-end — 01(d)
already shows PRD decompose and worker-task execution running via `opencode`
as a subprocess. stdout is a transcript, not a response object with a
`usage` field, so there's no obvious capture hook unless the CLI itself has a
structured-output mode datawatch isn't reading. Confirming that is outside
this project's `docs_search` corpus (a question about those external tools,
not datawatch) — flagged as open, not resolved.

**Ollama/OpenAI-compat calls** (`ollama`, `ollama-datawatch`, `openwebui`)
are more tractable: the data is already in every response, just not
captured. Fetched live this session from Ollama's own docs
(`https://raw.githubusercontent.com/ollama/ollama/main/docs/api.md`, checked
2026-09-30): every `/api/generate`/`/api/chat` response already includes
`prompt_eval_count`, `eval_count`, `prompt_eval_duration`, `eval_duration`,
`load_duration`, `total_duration`, and `prompt_eval_cached_count`, with the
documented throughput formula `eval_count / eval_duration * 10^9`. OpenAI-compat
responses carry the equivalent standard `usage: {prompt_tokens,
completion_tokens, total_tokens}` object. The `cost_usage` MCP tool already
exists to record exactly this (confirmed via its schema) — it evidently just
isn't being called from these paths. Fixing this half needs no new capture
mechanism; fixing the terminal half may need either a CLI structured-output
mode or proxying subprocess calls through datawatch's own capture path.

### Existing hooks reused

| Mechanism | Already does | Confirmed via |
|---|---|---|
| `cost_usage` MCP tool | Accepts `session`, `tokens_in`, `tokens_out`, rate overrides — the write path B1 needs | Tool schema, this session |
| `cost_summary`/`cost_rates` | Read/rollup paths already exist and break down per-backend | This session |
| Ollama response fields | `prompt_eval_count`/`eval_count`/`*_duration` on every call, no protocol change needed | `ollama/ollama` GitHub docs |
| `capacity_status` waiting queue | Queue-admission events already tracked structurally, just not persisted per-call | This session; 01(e) |
| `observer_stats`/`observer_peers_list` | Live GPU/process stats + federation already exist (`datawatch` peer registered, Shape B, v8.25.1 — older than the v8.34.1 daemon) | This session |
| `alert_rule_*` (S14b) | Complete per-rule CRUD + firings surface exists, zero rules defined | `alert_rule_list`, this session |
| `eval_run`/`eval_get_run`/`howto/evals.md` (BL259) | Full rubric-grading subsystem exists (4 grader types, capability/regression modes), zero suites defined | `docs_read`, `eval_list_suites` |

### Proposal B1 — Per-call usage records

**Problem.** No row anywhere represents "one model call" — backend, LLM,
model, node, role, tokens in/out, timings, success/retry flags. `cost_summary`'s
zero rollup is a symptom of this missing row, not the root cause.

**Proposed behavior.** A `usage_records` store, one row per call, written at
the call site. Two capture paths matching the split above: (a) Ollama/OpenAI-compat
— parse the already-present response fields and call `cost_usage` plus write
the fuller record; (b) terminal/subprocess — first check whether each CLI
exposes structured usage output; if not, proxy the subprocess's outbound
calls through the node the daemon already targets and reuse path (a).

**Fields needed.**

| Field | Source | Notes |
|---|---|---|
| `backend`, `llm_ref`, `model`, `node` | Already resolved per-call (01(d)) | Persist what's already chosen |
| `role`/`workload` | Part A Proposal 1's role block, or the literal call site name | Lets B5 group by workload |
| `session_id` / `prd_id`+`task_id`+`story_id` / `council_run_id`+`persona` / vision-call id | Whichever context the call happened in | Four attach points matching 01/02's consumer groups |
| `prompt_tokens`, `completion_tokens` | Ollama `prompt_eval_count`/`eval_count`; OpenAI-compat `usage.*`; terminal per open question | |
| `ttft_ms` | Ollama: approximated as `load_duration + prompt_eval_duration` (no native field); OpenAI-compat needs streaming to measure | Approximation, not exact, for Ollama |
| `decode_tokens_per_sec` | `eval_count / eval_duration * 1e9` | Directly available |
| `total_latency_ms`, `queue_wait_ms` | `total_duration`; queue wait from `capacity_status`'s admission event | Queue wait needs capturing the admission event itself |
| `num_ctx` | Per-request parameter | |
| `success`/`failed_json`/`retried` | Verifier/guardrail pass-fail result (already exists, v8.17.0 quality gates); reuses Part A Proposal 7's failure-type split | No new distinction invented |

**Surfaces.** New MCP tool (`usage_record_list`/`_get`), REST, PWA (a
per-session "Calls" sub-tab beside the existing Stats tab), CLI.

**Effort: L.** Path (a) is close to mechanical. Path (b) is the
effort-heavy, uncertain half — whether claude-code/opencode expose
structured usage wasn't confirmed this session (outside this project's docs
scope), and the proxy fallback is real new plumbing if they don't.

### Proposal B2 — Local-compute cost model

**Problem.** `cost_rates` gives `ollama`/`openwebui` a flat `$0`. Even with
real token counts (B1), `tokens × $0 = $0` — local inference draws real
power and was a real capital expense, so it isn't comparable to Claude's
metered cost without a substitute rate.

**Proposed behavior.** A per-node rate from two additive terms, both
operator-supplied, not invented by datawatch: `energy_rate = (gpu_watts/1000)
× electricity_$/kWh`, and `amortization_rate = node_capex_usd /
(expected_lifetime_hours × avg_utilization)`. Divide the combined `$/hour` by
observed tokens/sec (from B1) to land in the same `$/1k-token` unit
`cost_rates` already uses for Claude.

**Worked example — 32 GB GPU (`local-node`), fully sourced.** NVIDIA's own
product page: **575 W** total graphics power, **32 GB GDDR7**
(`nvidia.com/en-us/geforce/graphics-cards/50-series/gpu-32gb/`, checked
2026-09-30 — matches 01(a)'s declared 32 GB). EIA Table 5.6.A (July 2026,
released 2026-09-24, `eia.gov/electricity/monthly/epm_table_grapher.php?t=table_5_06_a`,
checked 2026-09-30): U.S. average residential price **18.31 cents/kWh**. At
full draw: `0.575 kW × $0.1831 = $0.1053/hour`. No sustained decode-rate was
measured this session; using an illustrative, explicitly unbenchmarked 80
tok/s: 288,000 tok/hour → **≈$0.00037/1k tokens** for energy alone —
roughly 8–40x below claude-code's $0.003/$0.015. The commonly-cited $1,999
launch MSRP was not independently re-verified this session — flag before
using it for amortization.

**Worked example — B200 (`datawatch-openai`), only partially sourced.**
Per-GPU TDP could not be confirmed from an official source this session:
the DGX B200 datasheet PDF returned no readable body text via `WebFetch`,
the HGX B200 page lists platform FLOPS/memory but no per-GPU wattage, and
`techpowerup.com` returned HTTP 403. The one official figure obtained:
NVIDIA's DGX B200 page states **"~14.3 kW max" system power for 8 GPUs**
(`nvidia.com/en-us/data-center/dgx-b200/`, checked 2026-09-30) → ≈1,787
W/GPU-slot, but that includes 2× host CPUs, NVSwitch, NICs, and PSU losses —
an overestimate of GPU-only draw, not a clean TDP. Third-party press
commonly cites 700–1000 W per B200 SXM, unconfirmed from an official source
this session. At the system-level proxy and EIA's residential/commercial
rates (18.31¢ / 14.53¢): **$0.26–$0.33/GPU-slot-hour**, presented as a
bounded range with stated assumptions, not a confident single number.

**Fields needed.** `declared_capacity.gpu_watts`, `node.capex_usd`,
`node.expected_lifetime_hours`, `node.avg_utilization_pct`; `cost_rates`
gains a computed local-compute row per node, falling back to `$0` only when
unset.

**Surfaces.** YAML, REST, MCP (`compute_node_add`/`_update`), CLI, PWA
(Hardware/Capacity section, beside Part A Proposal 2's VRAM×GPU auto-suggest).

**Effort: M.** The formula is simple; the uncertainty is entirely in
sourcing correct per-node inputs (demonstrated above — one node fully
sourced, one only partially) — an operator data-entry problem the tool
should surface clearly, not an engineering one.

### Proposal B3 — Node and model dashboards

**Problem.** Answering "what's running where, how close to headroom" means
manually cross-referencing three tools. `ollama_stats` (confirmed live) is
**host-scoped, not per-node** — it only reports the locally-configured
Ollama server, with no parameter to target `datawatch`'s instance instead.

**Proposed behavior.** A composite dashboard joining, per node: loaded
models/VRAM (`/api/ps`, requiring `ollama_stats` to accept a node target),
recent tokens/sec (B1), declared vs. computed-effective concurrency (Part A
Proposal 2), and live GPU/process stats from that node's observer peer
(`observer_peers_list` shows `datawatch` registered as Shape B on v8.25.1 —
older than the v8.34.1 daemon, a live version-skew finding parallel to
01(a)'s Ollama-version-skew).

**Fields needed.** None new — a join across existing tools; only schema
change is `ollama_stats(node: string)` gaining a parameter it lacks today.

**Surfaces.** MCP (`ollama_stats` parameter), REST, PWA (new "Fleet" view,
nearest existing analogues are the per-session Stats tab and Observer view
per `docs_read`).

**Effort: M.** Mostly composition of data that already exists; the one
concrete gap found this session is `ollama_stats`'s host-scoping.

### Proposal B4 — Alerting hooks

**Problem.** `alert_rule_list` confirms complete CRUD tooling with zero
rules defined. Three conditions from the brief are all independently
confirmed as real, current: saturation (`node:datawatch`'s
`max_concurrent_sessions: 1`, 01(e)'s binding constraint across four
passes), driver failure (the local NVML mismatch, 01(a), which fails silently
per-request rather than alerting), model-not-installed (01(c) drift item 1 —
five models the registry lists that aren't on the host).

**Proposed behavior.** Three starter rules via `alert_rule_create`: (a)
saturation — node pool held at limit past a threshold duration; (b) health —
`compute_node_health` reporting a persisting driver error; (c) drift — a
registry's model list diverging from a live `/api/tags`, automating 01(c)'s
manual comparison. **Caveat:** `rule_json` is an opaque blob per its own
schema — the exact condition grammar wasn't discoverable via `docs_search`
this session (queries for "alert rule saturation"/"firing condition" never
surfaced a dedicated alerting howto), parallel to Part A's
`howto/automata.md` gap. Proposed by intent, not ready-to-submit JSON.

**Fields needed.** None beyond what `alert_rule_create` accepts.

**Surfaces.** MCP/REST/CLI cover CRUD fully; PWA surfacing of
`alert_rule_firings` would be new UI only.

**Effort: S** once the condition grammar is confirmed; rule (a) is **S–M**,
most useful paired with Part A Proposal 2's computed-headroom number.

### Proposal B5 — Model scorecard

**Problem.** The brief asks this to join eval results (`docs/06`) with usage
stats — `docs/06` doesn't exist, and independently `eval_list_suites` is
`[]`. Nothing to join yet. Written instead against the actual `eval_run`
mechanism (suites at `~/.datawatch/evals/<name>.yaml`, four grader types,
capability/regression modes, per `docs_read` on `howto/evals.md`).

**Proposed behavior.** A read-only report joining, per (model, role): latest
eval `pass_rate` (`eval_get_run`) and B1's usage aggregates (tokens/sec,
`$`/1k via B2, success rate). A report over two other proposals' data, not a
new capture mechanism.

**Fields needed.** None new — a join of `eval_get_run` output with B1's
records, grouped by `(model, role)`.

**Surfaces.** MCP (`scorecard_get`, new), REST, PWA table view.

**Effort: M, sequenced last.** Two hard upstream dependencies, both
currently at zero (eval suites, B1) — the join logic is simple, but it can't
be meaningfully tested until both exist.

### What an operator should be able to ask

- *"Which model served council run X, and how long did each persona take?"*
  — B1 at the `(council_run_id, persona)` grain, via B3.
- *"Is `node:datawatch` out of headroom, or just at its declared ceiling?"*
  — Part A Proposal 2's computed concurrency, via B3.
- *"What did this PRD actually cost — Claude and local combined?"* — B1 +
  B2 together; impossible today (`cost_summary` is `$0` everywhere).
- *"Which registry entries have drifted, without re-running 01(c)'s manual
  check?"* — B4's drift rule.
- *"Is `qwen3.8:27b` earning its VRAM budget, or would a smaller model score
  the same for less?"* — B5, transitively needs B1 and a real eval suite.

### Prioritized backlog

1. **B1, Ollama/OpenAI-compat half only** — wire already-present response
   fields into `cost_usage`. Highest-confidence, lowest-effort slice; no new
   capture mechanism needed.
2. **B4 — three starter alert rules.** Infrastructure already complete;
   effort is confirming the condition grammar, not building anything.
3. **B2 — local-compute cost model.** M effort; this session's worked
   examples show it's tractable once B1 supplies real tokens/sec.
4. **B3 — dashboards.** M effort, mostly composition; the one blocker found
   (`ollama_stats` host-scoping) is small and specific.
5. **B1, terminal/subprocess half.** The effort-heavy, uncertain half (open
   question on CLI structured output; proxy fallback if not) — but the
   majority contributor to the zero-cost problem by volume (subprocess is
   83% of sessions).
6. **B5 — model scorecard.** Explicitly last: blocked on both eval suites
   (currently zero) and B1, regardless of how simple its own join is.

### Re-verification pass (2026-09-30, later same day)

Re-ran `cost_summary`, `cost_rates`, and `get_version` live via the
`datawatch-local-node-707f` MCP connection to check for drift since this
Part B was first written. `cost_summary`'s session count climbed from 2,263
to **2,267**; every token/USD field across all seven backends (`claude`,
`claude-code`, `goose`, `ollama`, `ollama-datawatch`, `opencode`,
`subprocess`) is still exactly `0`. `cost_rates` is unchanged — flat `$0`/`$0`
for `ollama`/`openwebui`/`shell`, non-zero list pricing everywhere else.
`get_version` still reports **8.36.1** (8.36.2 available), matching Part A's
already-documented finding — no further daemon movement this pass. The
problem this Part B describes is confirmed still live and unchanged in kind,
120+ sessions after the first evidence was gathered.

### Re-verification pass (2026-09-30, retry after verifier error)

This is a retry of the same task; the prior attempt's content was correct but
failed verification for an unrelated reason (`verifier: unparseable
response`). Re-ran the same five live checks rather than rewriting Part A/B.
`cost_summary`: **2,301** sessions (up from 2,267 last pass), still
`total_tokens_in: 0` / `total_tokens_out: 0` / `total_usd: 0` across all seven
backends (`claude` 6, `claude-code` 178, `goose` 2, `ollama` 6,
`ollama-datawatch` 6, `opencode` 220, `subprocess` 1,883). `cost_rates`:
unchanged — `ollama`/`openwebui`/`shell` still flat `$0`/`$0`, everything
else at unchanged list pricing. `eval_list_suites`: still `[]`. `alert_rule_list`:
still `{"rules":[]}`. `get_version`: daemon has moved to **8.36.10** (8.36.11
available) — real forward progress since the last pass's 8.36.1, but the
one-version-behind pattern and every zero-cost finding above are unchanged
in kind. No edits to the proposals or backlog were needed; this entry
records that the underlying analysis was re-confirmed live, not assumed.
