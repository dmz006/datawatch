# Open-model matrix eval — run 20261015-r8

Automaton: `d07570dd`  
Preflight passed: **True**

## Preflight

| role | llm | expected | used | ok |
|---|---|---|---|---|
| cpu | opencode-node-a-cpu | qwen3:1.7b | qwen3:1.7b | yes |
| fast | opencode-node-a-fast | granite4.2:8b | granite4.2:8b | yes |
| mid | opencode-node-a-gpu | granite4.2:30b | granite4.2:30b | yes |
| decompose-validate | opencode-node-b-gpu | gpt-oss:120b | gpt-oss:120b | yes |

## Wrong model cells

- fast-analysis: expected `granite4.2:8b`, used `None` (unknown; self-report `None`)
- fast-scripting: expected `granite4.2:8b`, used `None` (unknown; self-report `None`)
- fast-docs: expected `granite4.2:8b`, used `None` (unknown; self-report `None`)
- fast-review: expected `granite4.2:8b`, used `None` (unknown; self-report `None`)

## Suspect integrity cells

- cpu-analysis: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r8/cpu-analysis`, resolved `None`
- cpu-scripting: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r8/cpu-scripting`, resolved `None`
- cpu-docs: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r8/cpu-docs`, resolved `None`
- cpu-review: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r8/cpu-review`, resolved `None`
- fast-analysis: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r8/fast-analysis`, resolved `None`
- fast-scripting: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r8/fast-scripting`, resolved `None`
- fast-docs: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r8/fast-docs`, resolved `None`
- fast-review: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r8/fast-review`, resolved `None`
- mid-analysis: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r8/mid-analysis`, resolved `None`
- mid-scripting: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r8/mid-scripting`, resolved `None`
- mid-docs: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r8/mid-docs`, resolved `None`
- mid-review: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r8/mid-review`, resolved `None`
- decompose-validate-analysis: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r8/decompose-validate-analysis`, resolved `None`
- decompose-validate-scripting: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r8/decompose-validate-scripting`, resolved `None`
- decompose-validate-docs: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r8/decompose-validate-docs`, resolved `None`
- decompose-validate-review: task_project_dir `.datawatch/eval/open-model-matrix/20261015-r8/decompose-validate-review`, resolved `None`

## Model results

Trustworthy cells only (identity ok AND integrity ok); `—` = untrusted/not run.

| role | analysis | scripting | docs | review |
|---|---|---|---|---|
| cpu | — | — | — | — |
| fast | — | — | — | — |
| mid | — | — | — | — |
| decompose-validate | — | — | — | — |

## Notes on suspect cells (observed `resolved_dir` vs `task_project_dir`)

Every cell above is suspect for the same reason: `resolved_dir` is `None`, not a
different directory. By the time `collect` ran, the worker and verifier session
records were gone from `/api/sessions` (the list trims terminal sessions) and
could not be fetched by id either (`/api/sessions/<id>` → 404, and
`session_output` → "not found"). So the driver could not prove each worker's
cwd, and per the rules it leaves `model_result` null. No cell shows any sign of
cross-cell writes: every cell's own repo is either untouched or holds only that
cell's work, and every task's `project_dir` was set to its own cell
(`set_task_project_dir` decisions on the Automaton). Per-cell evidence (from the
cell dir and `verify-logs/`, both in the scratch run dir):

| cell | worker lifetime | MODEL-IDENTITY.txt | cell repo | verifier |
|---|---|---|---|---|
| cpu-analysis | ~45s | empty | no change | exit 1 |
| cpu-scripting | ~6m | empty | 1 uncommitted file | exit 1 |
| cpu-docs | ~50s | `dmz_model` (placeholder, not a model name) | no change | exit 1 (MODEL-IDENTITY.txt outside docs/) |
| cpu-review | ~70s | `model-name` (placeholder) | no change | exit 1 |
| fast-analysis | ~3s | absent | no change | exit 1 |
| fast-scripting | ~4s | absent | no change | exit 1 |
| fast-docs | ~4s | absent | no change | exit 1 (no changes in cell) |
| fast-review | ~4s | absent | no change | exit 1 |
| mid-analysis | ~7s | absent | no change | exit 1 (tracker_test.go missing) |
| mid-scripting | ~4s | absent | no change | exit 1 |
| mid-docs | ~6s | absent | no change | exit 1 |
| mid-review | ~7s | absent | no change | exit 1 |
| decompose-validate-analysis | ~21m | `gpt-oss:120b` | 2 uncommitted files, no commit | exit 2 (tracker.go does not compile) |
| decompose-validate-scripting | ~14m | `gpt-oss:120b` | no change | exit 1 |
| decompose-validate-docs | ~32m | `gpt-oss:120b` | 2 uncommitted files, no commit | exit 1 |
| decompose-validate-review | ~9m (2nd run) | `gpt-oss:120b` | 1 commit (`review-verdict.json`) | exit 1 (candidate-pass judged `fail`) |

Fast-role `model_identity` is `unknown` (shown under "Wrong model cells" by the
report) only because the post-run `llm test` for the fast entry timed out
during `collect`; the preflight for the same entry passed. None of the fast
cells did any work.

## Run verdict: INVALID for scoring (no trustworthy cell)

- **fast and mid roles (8 cells): infrastructure failure.** Each worker died 3–7s
  after spawn without writing MODEL-IDENTITY.txt (its required first action).
  This matches the r6 failure mode: opencode exits before the model gets a
  turn. These are not model failures.
- **cpu role (4 cells):** qwen3:1.7b did run (sessions lasted 45s–6m) but
  produced placeholder identity strings and no commits. That would likely
  score as fail, but it is unscored here because integrity could not be proven.
- **decompose-validate role (4 cells):** gpt-oss:120b ran for real and
  self-reported correctly, but no cell passed its verifier (broken compile,
  uncommitted work, wrong review verdict). Unscored for the same integrity
  reason.

## Operator interventions during the run

- The executor runs tasks one at a time in task-id order. At 06:12 the
  Automaton went `blocked` on decompose-validate-review: the executor flagged
  "foreign file drift" because the worker committed `review-verdict.json`.
  That file is the output the review spec requires the worker to commit, so
  the flag was a false positive. At that point the 6 tasks with letter-prefixed
  ids (all cpu cells, fast-analysis, fast-scripting) had never started.
  `reset_task` on that task reopened the Automaton; the review task re-ran (its
  second attempt is the one recorded) and the remaining 6 then ran. The PRD
  could not be re-approved or re-run while `blocked`.

## Follow-ups (not actioned here)

1. `collect` should keep each session record (or capture it at task-terminal
   time), because both the list and by-id lookups lose it.
2. Find the cause of the 3–7s opencode deaths on the node-a fast/mid entries
   (preflight `llm test` passes, but real sessions die immediately).
3. The file-drift guard should not block on files the task spec requires the
   worker to commit.
