# BL449 — Open-model 16-cell matrix runner (BL437 S5)

**Status:** In progress · **Date:** 2026-10-15 · **Tracker:** BL449 · **Implements:** BL437 S5 ([design doc](2026-10-14-recurring-model-eval-design.md))

Filed as autonomous PRD `9e7bad8e`. This doc records scope, status and
history. It was written after the fact, because the work began without a
plan doc of its own.

## Scope

A 4 roles × 4 task types = 16-cell eval of the open (opencode/Ollama)
models, run through the Automaton system. It reuses the model-tiering
fixtures unchanged (`fixtures_from: testdata/eval/model-tiering`). Each
cell records three verdicts that are never collapsed into one:

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
