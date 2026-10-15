# Open-model matrix eval — run 20261015-r4

Automaton: `09de0730`  
Preflight passed: **True**

## Preflight

| role | llm | expected | used | ok |
|---|---|---|---|---|
| cpu | opencode-johnnyjohnny-cpu | qwen3:1.7b | qwen3:1.7b | yes |
| fast | opencode-johnnyjohnny-fast | granite4.2:8b | granite4.2:8b | yes |
| mid | opencode-johnnyjohnny-gpu | granite4.2:30b | granite4.2:30b | yes |
| decompose-validate | opencode-datawatch-gpu | gpt-oss:120b | gpt-oss:120b | yes |

## Wrong model cells

- cpu-analysis: expected `qwen3:1.7b`, used `qwen3:1.7b` (unknown; self-report `None`)
- cpu-scripting: expected `qwen3:1.7b`, used `qwen3:1.7b` (unknown; self-report `None`)
- cpu-docs: expected `qwen3:1.7b`, used `qwen3:1.7b` (unknown; self-report `None`)
- cpu-review: expected `qwen3:1.7b`, used `qwen3:1.7b` (unknown; self-report `None`)
- fast-analysis: expected `granite4.2:8b`, used `granite4.2:8b` (unknown; self-report `None`)
- fast-scripting: expected `granite4.2:8b`, used `granite4.2:8b` (unknown; self-report `None`)
- fast-docs: expected `granite4.2:8b`, used `granite4.2:8b` (unknown; self-report `None`)
- fast-review: expected `granite4.2:8b`, used `granite4.2:8b` (unknown; self-report `None`)
- mid-analysis: expected `granite4.2:30b`, used `granite4.2:30b` (unknown; self-report `None`)
- mid-scripting: expected `granite4.2:30b`, used `granite4.2:30b` (unknown; self-report `None`)
- mid-docs: expected `granite4.2:30b`, used `granite4.2:30b` (unknown; self-report `None`)
- mid-review: expected `granite4.2:30b`, used `granite4.2:30b` (unknown; self-report `None`)
- decompose-validate-analysis: expected `gpt-oss:120b`, used `gpt-oss:120b` (unknown; self-report `None`)
- decompose-validate-scripting: expected `gpt-oss:120b`, used `gpt-oss:120b` (unknown; self-report `None`)
- decompose-validate-docs: expected `gpt-oss:120b`, used `gpt-oss:120b` (unknown; self-report `None`)
- decompose-validate-review: expected `gpt-oss:120b`, used `gpt-oss:120b` (unknown; self-report `None`)

## Suspect integrity cells

- cpu-analysis: task_project_dir `/home/dmz/workspace/datawatch/.datawatch/eval/open-model-matrix/20261015-r4/cpu-analysis`, resolved `None`
- cpu-scripting: task_project_dir `/home/dmz/workspace/datawatch/.datawatch/eval/open-model-matrix/20261015-r4/cpu-scripting`, resolved `None`
- cpu-docs: task_project_dir `/home/dmz/workspace/datawatch/.datawatch/eval/open-model-matrix/20261015-r4/cpu-docs`, resolved `None`
- cpu-review: task_project_dir `/home/dmz/workspace/datawatch/.datawatch/eval/open-model-matrix/20261015-r4/cpu-review`, resolved `None`
- fast-analysis: task_project_dir `/home/dmz/workspace/datawatch/.datawatch/eval/open-model-matrix/20261015-r4/fast-analysis`, resolved `None`
- fast-scripting: task_project_dir `/home/dmz/workspace/datawatch/.datawatch/eval/open-model-matrix/20261015-r4/fast-scripting`, resolved `None`
- fast-docs: task_project_dir `/home/dmz/workspace/datawatch/.datawatch/eval/open-model-matrix/20261015-r4/fast-docs`, resolved `None`
- fast-review: task_project_dir `/home/dmz/workspace/datawatch/.datawatch/eval/open-model-matrix/20261015-r4/fast-review`, resolved `None`
- mid-analysis: task_project_dir `/home/dmz/workspace/datawatch/.datawatch/eval/open-model-matrix/20261015-r4/mid-analysis`, resolved `None`
- mid-scripting: task_project_dir `/home/dmz/workspace/datawatch/.datawatch/eval/open-model-matrix/20261015-r4/mid-scripting`, resolved `/home/dmz/workspace/datawatch/.datawatch/eval/open-model-matrix/20261015-r4/mid-scripting`
- mid-docs: task_project_dir `/home/dmz/workspace/datawatch/.datawatch/eval/open-model-matrix/20261015-r4/mid-docs`, resolved `None`
- mid-review: task_project_dir `/home/dmz/workspace/datawatch/.datawatch/eval/open-model-matrix/20261015-r4/mid-review`, resolved `None`
- decompose-validate-analysis: task_project_dir `/home/dmz/workspace/datawatch/.datawatch/eval/open-model-matrix/20261015-r4/decompose-validate-analysis`, resolved `None`
- decompose-validate-scripting: task_project_dir `/home/dmz/workspace/datawatch/.datawatch/eval/open-model-matrix/20261015-r4/decompose-validate-scripting`, resolved `None`
- decompose-validate-docs: task_project_dir `/home/dmz/workspace/datawatch/.datawatch/eval/open-model-matrix/20261015-r4/decompose-validate-docs`, resolved `None`
- decompose-validate-review: task_project_dir `/home/dmz/workspace/datawatch/.datawatch/eval/open-model-matrix/20261015-r4/decompose-validate-review`, resolved `None`

## Model results

Trustworthy cells only (identity ok AND integrity ok); `—` = untrusted/not run.

| role | analysis | scripting | docs | review |
|---|---|---|---|---|
| cpu | — | — | — | — |
| fast | — | — | — | — |
| mid | — | — | — | — |
| decompose-validate | — | — | — | — |

## Run invalid: worker launch failure (no model was exercised)

**This run has no model results.** Preflight passed for all 4 entries (after
the BL449 preflight-timeout raise in `fe6ab76d`), the 16 cells were seeded, and
scratch Automaton `09de0730` ran. All 16 tasks still ended `failed`
("verification failed after retries") within **2 minutes** of start, because no
worker ever reached a model:

- For each task the daemon spawned an opencode worker (2 attempts per task,
  ~3 s each) and wrote a project `opencode.json` into the cell with a bare
  model name, e.g. `{"model": "gpt-oss:120b"}` (`cmd/datawatch/main.go:1497`,
  `model := sess.Model`, log line `[opencode] wrote project config
  (model=gpt-oss:120b …)`).
- opencode expects `<provider>/<model>`. It parses the bare name as provider
  `gpt-oss:120b` with an empty model and aborts before any LLM call:
  `ProviderModelNotFoundError: Model not found: gpt-oss:120b/.` The worker
  transcript shows `Error: {"name":"UnknownError",…}` (refs `err_fe042be7`,
  `err_54077655`). This was reproduced by running `opencode run` with
  `--print-logs` against a copy of the same `opencode.json`.
- `llm_test` (preflight) does not go through this path, which is why preflight
  passed while every worker failed.

**Manual overrides to the collected data** (the original collect output is kept
in the gitignored `state.json.pre-override`):

- `model_identity` is `unknown` for all 16 cells. The collector's `ok` came from
  a *post-run* `llm_test` (and, for mid-scripting, from the session record's
  configured model). That evidence doesn't show a cell ran on its model, and
  in fact no cell ran on any model.
- `mid-scripting` was collected as integrity `ok` / result `fail`. It is
  re-marked integrity `suspect`, result `null`. Its verifier exit 1 just
  reflects an untouched baseline (0 new commits) after the launch crash. It is
  not a granite4.2:30b verdict.
- `model_result` is `null` for every cell. No cell is scored pass or fail.

### Suspect cell notes (resolved_dir vs task_project_dir)

- **15 cells** (every cell except mid-scripting): `resolved_dir` = `None`,
  `task_project_dir` = `.datawatch/eval/open-model-matrix/20261015-r4/<role>-<type>`.
  Both worker sessions per task were killed and reaped within seconds, so the
  collector had no surviving session record to read `project_dir` from. The
  daemon did launch in the right dir: it wrote `opencode.json` into each cell,
  and the worker `cd`'d into it. The mismatch comes from the crash, not from
  cell escape.
- **mid-scripting**: `resolved_dir` = `task_project_dir` =
  `.datawatch/eval/open-model-matrix/20261015-r4/mid-scripting`. Suspect only
  because of the launch crash described above.

### Before re-running

The opencode project-config writer must emit a provider-qualified model, plus
a provider block pointing at the right Ollama host for remote-node entries. A
re-run before that fix will fail the same way. This report does not include
that fix.
