# BL449 — Open-model 16-cell matrix runner (BL437 S5)

**Status:** Deferred until after v10.0.0 ships (operator decision, 2026-10-18)
— scope expanded below to be fully inclusive before that round runs; no
infra/driver work resumes until the release is out. · **Date filed:**
2026-10-15 · **Tracker:** BL449 · **Implements:** BL437 S5 ([design
doc](2026-10-14-recurring-model-eval-design.md))

Filed as autonomous PRD `9e7bad8e`. This doc records scope, status and
history. It was written after the fact, because the work began without a
plan doc of its own.

## 2026-10-18 scope expansion (pre-work for the post-10.0.0 round)

Operator asked to fold the standalone Swift-1.5/coder-model question (see
[[project_open_model_decompose_findings]] Finding 2, and the 2026-10-18
research turn) into this matrix as real cells rather than a separate
one-off test, and to make the matrix's candidate list "fully inclusive"
before that round runs. Additions, not yet executed:

- **New execution-role candidates** (`datawatch` ComputeNode, confirmed
  present via `ollama show` 2026-10-18, all Q4_K_M):
  - `swift-1.5-qwen38-27b:latest` (qwen35 family) — UkisAI's reasoning-
    efficient Qwen3.8-27B tune. Vendor claims are scoped to coding/agent
    work (LiveCodeBench v6, Terminal-Bench 2.1); it trails the base model
    on IFBench/ERQA/AIME/HMMT. **Execution-role candidate only** — do not
    add it as a verify or decompose candidate; nothing in the vendor's own
    data supports that role, and Finding 1 (headless-ollama can't
    decompose a tooled PRD) is a harness constraint that applies
    regardless of which model sits behind it.
  - `qwen3-coder:30b` (qwen3moe) — coder-specialized, not a reasoning-
    efficiency tune. Newly present on the node since the last matrix
    touch; never tested here.
  - `qwen3-coder-next:q4_K_M` (qwen3next, 79.7B) — larger coder-
    specialized variant, same family. Newly present; never tested.
  - `qwen3.5:35b` (qwen35moe) — newer Qwen generation, general-purpose.
    Newly present; never tested.
  - Baseline stays `qwen3.8:27b` (the existing confirmed-good execution
    model, [[project_open_model_decompose_findings]]).
- **Why fold Swift/coder in here instead of a separate script**: the
  existing driver's 3-verdict split (`model_identity` /
  `verification_integrity` / `model_result`) plus its per-cell dual-
  verdict design already gives a correctness-controlled comparison;
  redoing that machinery standalone for just Swift would duplicate work
  the matrix driver is built to do once it's unblocked. The prior Swift
  test ([[project_open_model_decompose_findings]] Finding 2) was single-
  sample, no reasoning-effort control, on a contended node, and compared
  against a mixed task basket — not a real substitute for a matrix-grade
  run.
- **Reasoning effort / thinking level control — now real, via BL492
  (correction: an earlier revision of this section said no such tracker
  item existed; the supervisor filed it minutes later as README row 74,
  draft PRD `f888d75b`, status `draft` as of 2026-10-18).** BL492 is
  "make local-model (pi) workers observable and less prone to stalling,"
  not a matrix feature in itself, but it is the source of the knob this
  round needs:
  - **`pi.thinking_level`** — new setting, default empty (= pi's own
    default). Allowed values: `off`, `minimal`, `low`, `medium`, `high`.
    Passed to the pi worker as its `--thinking` flag. Has a per-PRD and
    per-task override, same resolution shape as other per-unit LLM
    settings (PRD > task > global). Full parity surface per BL492's spec:
    REST/MCP/CLI/comm config get/set, PWA settings, `docs/config-
    reference.yaml`, `docs/operations.md`, with locale keys in
    de/en/es/fr/ja.
  - **`autonomous.loop_detect_turns`** (default 3, `0` = off) — a
    related but separate BL492 setting: fires a "looping" warning (and,
    for one-shot print sessions, stops+retries with a hint) when the
    last N assistant turns are announce-only or repeat an identical tool
    call. Not a reasoning-effort knob, but relevant to this matrix
    because a stuck/looping cell should be classified as that, not as a
    silent `model_result` failure.
  - BL492 also adds `--mode json`/`rpc` pi launch + a progress reader
    over `~/.pi/agent/sessions/<project>/<time>_<id>.jsonl` (last-event
    heartbeat, tool-call counts, last-thinking-block summary) — useful
    for this matrix's own debugging of a stuck cell, not a scoring input.
  - **BL492 is itself still `draft`** (not yet decomposed/built as of
    2026-10-18) and is explicitly sequenced to be exercised by this
    matrix *after* v10.0.0, per the supervisor's own note on README row
    74 ("tested in the open-model matrix after v10.0.0, not now"). So
    this matrix round depends on BL492 shipping first — `pi.thinking_level`
    has to exist and work before a cell can pin it.
  - Swift/qwen3-coder cells (the new execution-role candidates above) run
    via the **opencode** backend in this matrix, not pi — confirm at
    implementation time whether `pi.thinking_level` is pi-only or also
    reaches the opencode→Ollama path's own `think` request field; if it's
    pi-only, the opencode-backed cells need their own equivalent knob
    (Ollama's native per-request `think: true/false`/level) rather than
    inheriting BL492's setting unchanged.
- **Methodology fixes for this round, regardless of model list**: multi-
  sample per cell (the matrix driver already loops cells; just don't
  short-circuit to n=1), and pin `pi.thinking_level` (or the opencode-
  path equivalent) explicitly per cell rather than leaving it at the
  model's default — mirrors the gap that made Finding 2 inconclusive.
- **Still blocking before any of this can run** (r8 follow-ups from the
  table below, unresolved as of 2026-10-18 — confirmed via git log, zero
  commits touching the driver or its fixtures since 2026-10-16): node-a
  fast/mid opencode sessions dying 3-7s after spawn even post-v9.1.4,
  and the file-drift guard blocking on files a task spec requires the
  worker to commit. New candidate models do not change this — they ride
  the same broken opencode-session-start path until it's fixed.

## 2026-10-18 precondition: 64k context

Every cell must run with a 64k context (`num_ctx` 65536). The 32k default overflowed
(`stopReason=length`) and made large diffs unverifiable, so context size is a recorded
axis of each cell, not a background detail. Record the effective context window, the model
name and digest, the node, and the keep-alive setting with each result; discard or flag a
result that ran at 32k. See [model host requirements](2026-10-18-model-host-requirements.md).

## 2026-10-18 backend switch: pi primary, opencode kept as an open question

Operator instruction (same day, after the above): pi has proven more
stable than opencode for open-model work generally (fewer stalls, better
completion detection — see [[project_opencode_acp_switch_findings]] and
the r4/r6/r8 opencode-session-death history above). **Switch this
matrix's default execution path to pi for every cell**, superseding the
"Swift/coder cells run via opencode" assumption two sections up — all 16
cells, baseline and new execution candidates alike, run through
pi-prompt by default now. This also sidesteps the still-blocking node-a
opencode-session-death bug for the matrix's primary (pi) scoring, though
it does not fix that bug.

**Opencode is not dropped, only demoted to a secondary question.**
Operator's own framing: "we should also test on opencode since we are
testing supported tooling maybe" — part of what this matrix is for is
confirming which backends a given open model actually runs under, not
only which backend is fastest. So each cell's design should leave room
for an optional opencode run of the same model/task pair for
comparison, but **whether to run it, and for which cells, is a question
for the supervisor/PRD to settle when this round actually starts**
(after v10.0.0, after BL492 ships) — not decided in this doc. Reasons to
leave it open rather than commit now:
- opencode's own node-a session-death bug (r4/r6/r8, unresolved) may
  still be unfixed by the time this round runs, which would make an
  opencode lane pure noise regardless of which model is under test;
- the `pi.thinking_level` vs. opencode→Ollama `think`-field question
  (two sections up) is still unresolved and bears directly on whether an
  opencode lane can even be scored on the same terms as the pi lane;
- running every cell twice (pi + opencode) doubles the round's GPU-node
  cost for a matrix that is already gated on node availability.

If opencode is kept as a comparison lane, it should **not** gate
`model_result` the way the pi lane does — it would be an
informational/secondary verdict column, so an opencode-side
infrastructure failure never blocks the matrix's primary (pi) scoring.

## Scope

A 4 roles × 4 task types = 16-cell eval of the open (Ollama-backed)
models, run through the Automaton system, backend **pi by default**
(2026-10-18 switch above) with opencode as an optional, non-gating
secondary lane pending the supervisor/PRD's call at round-start time. It
reuses the model-tiering fixtures unchanged (`fixtures_from:
testdata/eval/model-tiering`). Per the 2026-10-18 expansion above, the
`execution` role's candidate set for
the post-10.0.0 round grows to baseline `qwen3.8:27b` plus
`swift-1.5-qwen38-27b`, `qwen3-coder:30b`, `qwen3-coder-next:q4_K_M`, and
`qwen3.5:35b` — `decompose`/`verify`/other roles are unchanged, since
nothing in the vendor data or Finding 1's harness constraint supports
Swift or the coder variants there. Each cell records three verdicts
that are never collapsed into one:

- `model_identity`: did the cell run on the expected model?
- `verification_integrity`: did the task and verifier operate on the
  cell dir, not a parent?
- `model_result`: verifier pass/fail. It counts only when both of the
  above are `ok`.

Deliverables:

| What | Where |
|---|---|
| Fixtures (placeholder LLM/node names) | `testdata/eval/open-model-matrix/` |
| Driver: preflight / setup / create-prd / run / collect / report / self-test / cleanup | `scripts/eval_open_model_matrix.py` |
| Run reports | `docs/testing/runs/<date>-open-model-matrix/results.{json,md}` |

**Out of scope** (from the spec): changing defaults, daemon config or
automaton-level LLM settings, and modifying the BL422/BL447 Claude tiering
matrix (`scripts/eval_model_tiering.py`), which is a separate, parallel
effort. The open-model driver imports that module's HTTP client and PRD
helpers.

### Scope notes: two edits to `scripts/eval_model_tiering.py`

- `fe6ab76d` (BL449) adds an optional per-call `timeout` to the shared
  `Client.request/get/post`. The default (60 s) is unchanged, and no
  existing call site in the Claude driver changes behaviour. Only the
  open-model preflight `llm_test` call passes 240 s. This is a narrow
  enabling change to a shared helper. It is in scope as a dependency, not
  as a change to the Claude matrix.
- `b7f59ed6` (tagged BL447) changes the Claude driver's `cmd_create_prd`
  to call `set_task_project_dir` per cell. The same Claude session
  committed it while working on BL449. The repo records no operator
  sanction for it. It is **not BL449 work**: it is a follow-up to BL447
  and belongs to the parallel Claude-matrix effort. It stays in place,
  because reverting it would reintroduce the BL422 diff-capture bug that
  BL447 fixed. It is flagged here for the operator to accept or reassign.

## Status and history (2026-10-15)

| Run / commit | What happened |
|---|---|
| `3acd24c1`, `ce9d7b94` | Fixtures and driver added. |
| r1 (`bfafeb30`) | Preflight failed: `gpt-oss:120b` `llm_test` timed out at the client's 60 s read timeout. |
| — | BL451 rollup race left PRD `9e7bad8e` at `completed` with a pending task. Fixed in v9.1.1/v9.1.2 and unstuck. The PRD is `running` again. |
| r3 (`84ab59e4`) | Preflight failed: `granite4.2:30b` timeout, a cold-load race on the shared GPU node. |
| `fe6ab76d` | Preflight `llm_test` timeout raised to 240 s (per-call override; default unchanged). |
| r4 (`2c65e850`) | Preflight passed, run invalid: every opencode worker died in about 3 s with `ProviderModelNotFoundError`. |
| r5 (`6cf10468`) | Preflight failed: every `llm_test` 404'd because the registry model carries `ollama/` and the raw Ollama API rejects it. |
| v9.1.3 (`f9eb5970`) | Ask-path adapter strips `ollama/` before calling Ollama. Table test `TestOpenCode_Infer_ModelPrefix` added afterwards. |
| r6 (`78dfd70f`) | Preflight 4/4 OK, then `run_verdict = invalid-infrastructure`. All 16 workers hit `ProviderModelNotFoundError` because the driver pins the bare model name, so `opencode.json` got no `provider.ollama` block. No cell did any model work. |
| v9.1.4 (`f0c80d2e`) | `handleStartSession` gates the Ollama-URL resolution on the compute node's kind, not the model prefix, and normalises the model to `ollama/<model>` for opencode backends. Not yet exercised by a matrix run. |
| r8 (`3382fcda`, Automaton `d07570dd`) | Preflight 4/4 OK on v9.1.4, run invalid for scoring: 16/16 cells suspect. fast/mid workers (8 cells) died 3–7 s after spawn without writing `MODEL-IDENTITY.txt`, the r6 failure mode surviving v9.1.4. cpu (`qwen3:1.7b`) ran but wrote placeholder identities. decompose-validate (`gpt-oss:120b`) ran and self-reported correctly, but no cell passed its verifier. Session records were evicted before `collect`, so `resolved_dir` is unresolvable everywhere and `model_result` stays null. A false-positive file-drift block on decompose-validate-review (it committed the spec-required `review-verdict.json`) stalled the run until a `reset_task`. |
| Remediation | Leak fixes: placeholder names in the tracked fixtures, a gitignored local matrix override, and `report` redaction. Also the adapter test, testing doc, tracker and plan refresh. |

**Not yet done**

- A valid run. Spec steps 3–5 (per-cell dual verdict and model identity
  from real execution) have no valid data yet. The three-verdict
  separation exists in the driver, and its self-test passes, but no real
  run has exercised it. Findings from the only real round to date:

  ### Findings — pi-r1 (2026-10-29, results in
  `docs/testing/runs/2026-10-29-open-model-matrix/`)

  Preflight passed for four of the five tracked roles (`base`,
  `qwen3-coder`, `qwen3-coder-next`, `qwen3.5`; the `swift-1.5-qwen38-27b`
  role was not in the local matrix for this run). Every one of the 16
  cells ran at thinking level **`minimal`** (pinned per role). No cell
  produced a valid score: all 12 `qwen3-coder` / `qwen3-coder-next` /
  `qwen3.5` cells — analysis, scripting, docs and review alike — are
  **invalid, not model results**: the daemon placed each worker on a node
  that does not host its model, so the worker died about 12 s per attempt
  with a model-not-found 404, made no commit beyond the baseline, and was
  held for operator review after three failed attempts; there is no
  per-task node pin for the shared registry entry. The four `base`
  (`qwen3.8:27b`) cells ran to completion on all four task types, but
  their worker and verifier session records were already gone from the
  daemon at collect time, so `model_identity` is unknown (self-reports
  match the expected model but are not evidence) and
  `verification_integrity` is suspect — they are reported **untrusted**,
  not as pass/fail results. To get a valid run: declare which node hosts
  which model (or allow a per-task node pin), reset the held tasks, and
  collect while the session records are still present.
- ~~r6's remaining follow-ups~~ — **superseded by the pi-only scope**
  (2026-10-18 backend switch; the opencode lane is out of scope):
  - ~~an opencode-path preflight: one real `opencode run` per role~~;
  - ~~the opencode one-shot wrapper must not emit `DATAWATCH_COMPLETE` when
    `opencode run` fails~~;
  - `collect` must resolve worker sessions by id instead of the trimmed
    `/api/sessions` list — kept, and pi-lane-relevant (pi-r1's base cells
    lost their session records before collect).
- A unit test for the v9.1.4 `handleStartSession` normalisation.
- r8 follow-ups:
  - ~~find why node-a fast/mid opencode sessions die in 3–7 s even though
    preflight `llm_test` passes (v9.1.4 did not fix it)~~ — **superseded
    by the pi-only scope**; the r8 evidence remains history, and the pi
    lane has its own node-placement issue (Findings above) instead;
  - the executor's file-drift guard must not block on files the task spec
    requires the worker to commit.

**Attribution note:** v9.1.4 (`f0c80d2e`, `internal/server/api.go`,
`cmd/datawatch/main.go`, `CHANGELOG.md`) was committed by a parallel Claude
session, not by the BL449 remediation session. It is in BL449's history
because it fixes a BL449-found bug, but no BL449 remediation commit edits
`internal/server/api.go`.

## Leak handling

The tracked `matrix.json` carries placeholder registry and node names
(`opencode-node-a-cpu`, `node-a`, `node-b`, …). Real names live in
`.datawatch/eval/open-model-matrix/matrix.local.json` (gitignored), or in a
file named by `DATAWATCH_EVAL_OPEN_MATRIX`. Before `report` writes tracked
results, it maps local LLM names to the placeholders per role, maps `node`
fields the same way, rewrites in-repo absolute paths as repo-relative,
replaces any other home-dir path with `~`, and replaces the local username
with `<user>` (r8 leaked it through a model's self-reported identity
string). The self-test covers all of this.

## Parity surface

| Surface | Decision | Reason |
|---|---|---|
| REST | Excluded (consumer only) | The driver calls existing endpoints (`/api/llms/<name>/test`, autonomous PRD actions, `/api/sessions`). It adds no endpoint. |
| MCP | Excluded | No new tool. The equivalent MCP tools (`llm_test`, `autonomous_prd_*`) already exist. |
| CLI | Excluded | An offline eval script, not a `datawatch` subcommand. |
| comm channel | Excluded | Not operator-facing. |
| YAML/config | Excluded | No config field. The local matrix override is an eval-script input, not daemon config. |
| PWA | Excluded | No UI. Results are markdown/JSON under `docs/testing/runs/`. |
| Android | Excluded | No operator-visible change. |
| iPhone/iOS | Excluded | No operator-visible change. |

The daemon fixes found along the way (v9.1.3 ask adapter, v9.1.4 session
launch) change no contract on any surface. They make the existing
`llm_test` and opencode session-start paths accept the registry's
`ollama/<model>` form.
