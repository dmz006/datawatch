# Open-model matrix eval — run 20261015-r6

Automaton: `4199b08c`  
Preflight passed: **True**

## Preflight

| role | llm | expected | used | ok |
|---|---|---|---|---|
| cpu | opencode-node-a-cpu | qwen3:1.7b | qwen3:1.7b | yes |
| fast | opencode-node-a-fast | granite4.2:8b | granite4.2:8b | yes |
| mid | opencode-node-a-gpu | granite4.2:30b | granite4.2:30b | yes |
| decompose-validate | opencode-node-b-gpu | gpt-oss:120b | gpt-oss:120b | yes |

## Wrong model cells

None.

## Suspect integrity cells

- cpu-analysis: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r6/cpu-analysis`, resolved `None`
- cpu-scripting: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r6/cpu-scripting`, resolved `None`
- cpu-docs: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r6/cpu-docs`, resolved `None`
- cpu-review: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r6/cpu-review`, resolved `None`
- fast-analysis: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r6/fast-analysis`, resolved `None`
- fast-scripting: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r6/fast-scripting`, resolved `None`
- fast-docs: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r6/fast-docs`, resolved `None`
- mid-analysis: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r6/mid-analysis`, resolved `None`
- mid-scripting: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r6/mid-scripting`, resolved `None`
- mid-docs: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r6/mid-docs`, resolved `None`
- mid-review: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r6/mid-review`, resolved `None`
- decompose-validate-docs: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r6/decompose-validate-docs`, resolved `None`
- decompose-validate-review: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r6/decompose-validate-review`, resolved `None`

**Note on every suspect cell above.** `resolved_dir` is `None` (not a different
directory): the worker session record was no longer in `/api/sessions` when
`collect` ran (the list trims terminal sessions; 13 of the 16 worker sessions
were already evicted, 3 were still listed). Direct session lookups confirmed
each worker's `project_dir` equalled its `task_project_dir` (see the session
table below), so there was no cross-cell contamination — the cell is suspect
only because the driver could not prove it at collect time. `collect` should
fetch sessions by id (`/api/sessions/<id>`) instead of relying on the trimmed
list.

## Model results

Trustworthy cells only (identity ok AND integrity ok); `—` = untrusted/not run.

| role | analysis | scripting | docs | review |
|---|---|---|---|---|
| cpu | — | — | — | — |
| fast | — | — | — | fail |
| mid | — | — | — | — |
| decompose-validate | fail | fail | — | — |

## Run verdict: INVALID (infrastructure failure — no cell did any model work)

The 16 workers all died ~3 seconds after launch. Every session's terminal log
ends with opencode's

```
Error: {"name": "UnknownError", "data": {"message": "Unexpected server error. Check server logs for details.", "ref": "err_…"}}
DATAWATCH_COMPLETE: opencode done
```

Reproducing `opencode run` by hand in a scratch dir with `--print-logs` gives
the underlying error:

```
ProviderModelNotFoundError: Model not found: ollama/qwen3:1.7b. Did you mean: ollama-cloud?
```

### Root cause (observed, not fixed here)

1. opencode has no built-in Ollama provider. The daemon only writes the
   required `provider.ollama` block (npm `@ai-sdk/openai-compatible`, baseURL
   from the compute node) into the per-session `opencode.json` when the session
   model carries the `ollama/` prefix (`internal/llm/backends/opencode/lsp.go`
   `WriteProjectConfig`; `internal/server/api.go` resolves the Ollama URL on the
   same prefix test).
2. The LLM registry entries DO carry it (`ollama/qwen3:1.7b`,
   `ollama/granite4.2:8b`, `ollama/granite4.2:30b`, `ollama/gpt-oss:120b`;
   that is what v9.1.3 fixed for the ask/llm_test path by stripping the prefix
   there).
3. But the matrix driver pins each task's model explicitly via
   `set_task_llm {backend: <registry entry>, model: <expected_model>}` using
   the fixture's bare Ollama name (`qwen3:1.7b`, …). That bare task-level
   override wins over the registry entry's model, so the session is launched
   with `model=qwen3:1.7b`: no provider block, no Ollama URL, and opencode
   fails before the first turn. Daemon log for each session:
   `[opencode] wrote project config (model=qwen3:1.7b lsp=)`; the cell's
   `opencode.json` is exactly `{"model": "qwen3:1.7b"}`.
4. The opencode one-shot wrapper echoes `DATAWATCH_COMPLETE` unconditionally
   after `opencode run`, so the executor saw a "completed" worker, the verifier
   found an empty diff, the task was retried once (second session died the same
   way) and then failed with "verification failed after retries". Total wall
   time for all 16 cells: 2 minutes.

Preflight passed because `llm_test` goes through the daemon's ask adapter
straight to the Ollama HTTP API (prefix stripped, v9.1.3), so it proves the
models are loadable on their nodes but does not exercise the opencode session
launch path. The two paths currently require opposite model spellings.

### What this means for the cells

- `model_identity = ok` on all 16 only reflects the session record's `model`
  field (which the driver itself set); no model ever answered.
- The 3 cells scored `model_result = fail` (fast/review,
  decompose-validate/analysis, decompose-validate/scripting) have
  `new_commits = 0` and verifier output "no verdict / no changes": they are
  infrastructure failures, NOT model failures, and must not be counted against
  granite4.2:8b or gpt-oss:120b. `results.json` carries
  `run_verdict: invalid-infrastructure` for this reason.
- The 13 suspect cells are the eviction artifact described above.
- No model-quality conclusion can be drawn from this run.

### Worker sessions (first attempt / retry) — all `killed`, ~3s each

| cell | sessions | model on session record | project_dir == task_project_dir |
|---|---|---|---|
| cpu-analysis | 9903 / 849b | qwen3:1.7b | yes |
| cpu-scripting | 834a / dc60 | qwen3:1.7b | yes |
| cpu-docs | fb92 / 4d01 | qwen3:1.7b | yes |
| cpu-review | 1e60 / c7d0 | qwen3:1.7b | yes |
| fast-analysis | bcdd / 6e11 | granite4.2:8b | yes |
| fast-scripting | 24aa / 7cb2 | granite4.2:8b | yes |
| fast-docs | 24a0 / 870e | granite4.2:8b | yes |
| fast-review | 7b0a / ecb1 | granite4.2:8b | yes |
| mid-analysis | c7cb / 2cc9 | granite4.2:30b | yes |
| mid-scripting | a07f / a42d | granite4.2:30b | yes |
| mid-docs | 1091 / cead | granite4.2:30b | yes |
| mid-review | 5205 / 19df | granite4.2:30b | yes |
| decompose-validate-analysis | aece / 5343 | gpt-oss:120b | yes |
| decompose-validate-scripting | fe83 / 884d | gpt-oss:120b | yes |
| decompose-validate-docs | 7781 / 3847 | gpt-oss:120b | yes |
| decompose-validate-review | 6899 / 4d11 | gpt-oss:120b | yes |

### Needed before the next attempt (not done in this run; docs-only commit)

1. Make the opencode session launch accept the registry's `ollama/<model>`
   form end to end: either the driver pins `model: ollama/<expected>` (and
   compares `used_model` against the bare name, as now), or the daemon
   normalises a bare model for opencode-kind sessions by adding the prefix /
   writing the provider block whenever the resolved LLM entry is an Ollama
   compute-node entry. The second is the durable fix: a bare override on any
   opencode task currently produces an unlaunchable session.
2. Add an opencode-path preflight: one real `opencode run` per role (or a
   one-task scratch Automaton) must return a model answer before the matrix is
   built; `llm_test` alone is not sufficient.
3. The opencode one-shot wrapper should not emit `DATAWATCH_COMPLETE` when
   `opencode run` exits with an error, so a dead worker fails fast as a launch
   error instead of as "verification failed after retries".
4. `collect` should resolve worker sessions by id rather than from the trimmed
   `/api/sessions` list, so integrity is not marked suspect by eviction.

Cleanup: scratch Automaton `4199b08c` cancelled and the 16 cell dirs removed;
verify logs and `state.json` kept under
`.datawatch/eval/open-model-matrix/20261015-r6/` (gitignored).
