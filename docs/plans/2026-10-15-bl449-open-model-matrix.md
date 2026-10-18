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
- **Reasoning effort / thinking level control — BLOCKED on an unresolved
  reference.** The operator asked this round to document the reasoning/
  effort/thinking-level configuration "added to BL492." No such tracker
  item exists: not in this repo's code, docs, or CHANGELOG, and not in
  the live daemon's PRD registry (checked 2026-10-18; highest real item
  found is BL484). The only effort concept that exists today is
  `internal/autonomous/models.go`'s `Effort` type
  (`low/medium/high/max/quick/normal/thorough`), which maps to **claude-
  code's `--effort` flag** (BL422/BL41) — it has no connection to Ollama/
  qwen "thinking" control. There is currently no `reasoning_effort`/
  `thinking_level`/`think` setting anywhere in `internal/llm` or
  `internal/config`. **Do not design matrix cells around per-effort
  variants until this is resolved** — get the correct tracker ID/location
  from the operator, or confirm no such config exists yet and this matrix
  round should add it (new scope, not a reuse).
- **Methodology fixes for this round, regardless of model list**: multi-
  sample per cell (the matrix driver already loops cells; just don't
  short-circuit to n=1), and if/when a reasoning-effort knob exists,
  pin it explicitly per cell rather than leaving it at the model's
  default — mirrors the gap that made Finding 2 inconclusive.
- **Still blocking before any of this can run** (r8 follow-ups from the
  table below, unresolved as of 2026-10-18 — confirmed via git log, zero
  commits touching the driver or its fixtures since 2026-10-16): node-a
  fast/mid opencode sessions dying 3-7s after spawn even post-v9.1.4,
  and the file-drift guard blocking on files a task spec requires the
  worker to commit. New candidate models do not change this — they ride
  the same broken opencode-session-start path until it's fixed.

## Scope

A 4 roles × 4 task types = 16-cell eval of the open (opencode/Ollama)
models, run through the Automaton system. It reuses the model-tiering
fixtures unchanged (`fixtures_from: testdata/eval/model-tiering`). Per
the 2026-10-18 expansion above, the `execution` role's candidate set for
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
  run has exercised it.
- r6's remaining follow-ups:
  - an opencode-path preflight: one real `opencode run` per role;
  - the opencode one-shot wrapper must not emit `DATAWATCH_COMPLETE` when
    `opencode run` fails;
  - `collect` must resolve worker sessions by id instead of the trimmed
    `/api/sessions` list.
- A unit test for the v9.1.4 `handleStartSession` normalisation.
- r8 follow-ups:
  - find why node-a fast/mid opencode sessions die in 3–7 s even though
    preflight `llm_test` passes (v9.1.4 did not fix it);
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
