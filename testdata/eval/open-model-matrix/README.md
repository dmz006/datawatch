# Open-model matrix eval — fixtures

Test data only: nothing here is read by the daemon, and creating it changes no
default, daemon config or automaton-level LLM setting. It defines a second eval
grid over the open (pi-prompt/Ollama) models, reusing the Claude tiering eval's
task fixtures unchanged.

## Shape: 16 cells

4 roles × 4 task types = 16 cells, 1 rep each.

| Role | LLM registry entry (placeholder) | Model | Node (placeholder) |
|---|---|---|---|
| cpu | `pi-node-a-cpu` | `qwen3:1.7b` | node-a-cpu |
| fast | `pi-node-a-fast` | `granite4.2:8b` | node-a |
| mid | `pi-node-a-gpu` | `granite4.2:30b` | node-a |
| decompose-validate | `pi-node-b-gpu` | `gpt-oss:120b` | node-b |

The LLM/node names above are placeholders. Your own registry entry and
compute-node names go in a local matrix that is never committed:

1. Copy `matrix.json` to `.datawatch/eval/open-model-matrix/matrix.local.json`
   (gitignored), or anywhere and point `DATAWATCH_EVAL_OPEN_MATRIX` at it.
2. Replace each role's `llm` and `node` with your names. Keep `role`,
   `model`, `task_types`, `fixtures_from` and `cell_root` unchanged.

The driver loads `$DATAWATCH_EVAL_OPEN_MATRIX`, then the local file, then this
placeholder file. Run with only the placeholders and preflight fails, because
the names do not resolve. `report` maps the local names back to these
placeholders per role and makes in-repo paths repo-relative before it writes
`docs/testing/runs/…`, so tracked results never carry private hostnames or
home-dir paths.

Task types: `analysis`, `scripting`, `docs`, `review`.

**No effort axis.** `matrix.json` has `"effort_axis": null` on purpose: effort
is a claude-code-only control (see
`docs/plans/2026-10-14-recurring-model-eval-design.md`). Matrix cells pass no
effort anywhere.

## Reuse of the model-tiering fixtures

`"fixtures_from": "testdata/eval/model-tiering"`. Everything task-specific is
read from there and must not be copied or edited here:

| What | Path (under `testdata/eval/model-tiering/`) |
|---|---|
| Worker specs | `specs/<type>.md` |
| Per-cell verifiers | `verify/<type>.sh <cell-dir>` (+ `verify/hidden/` for analysis) |
| Review diffs | `review/candidate-pass.diff`, `review/candidate-fail.diff`, copied into the cell under the neutral names in `review/cell-names.json` |
| Baselines | `matrix.json` `task_types[].baseline_sha` |

Baselines as of this writing (authoritative source is the tiering `matrix.json`):

| Type | Baseline |
|---|---|
| analysis | `d2f30df2` |
| scripting | `54954c5f` |
| docs | `54954c5f` |
| review | `54954c5f` |

## Cell isolation

Mirrors the tiering eval's isolation (BL422):

- One cell dir per task: `.datawatch/eval/open-model-matrix/<run-id>/<role>-<type>`
  (`cell_root` in `matrix.json`), e.g. `…/<run-id>/mid-analysis`.
- Each cell is a fresh snapshot: `git archive <baseline_sha> | tar -x -C <cell>`,
  then `git init` + one baseline commit, so no other history is reachable.
- Seed the gitignored `internal/docsindex/assets/docs-bm25-index.json` with `{}`
  so `go build ./...` works, as the tiering README describes.
- The worker stays inside its cell dir, commits to the cell's own repo, and
  uses no memory recall, other branches or project history (the specs say so).

## Per-cell outcomes

Every cell records three independent outcomes:

1. **`model_identity`** — did the cell actually run on the role's model/node
   (e.g. pi/Ollama logs show `granite4.2:30b` on the mid role's node), or is it
   suspect / wrong-model (fallback, different model loaded, wrong node)?
2. **`verification_integrity`** — did the verifier run cleanly against an
   untampered cell (cell isolated, verifier and fixtures unmodified, verifier
   exit status trustworthy)?
3. **`model_result`** — the verifier's pass/fail for the work itself.

`model_result` only counts when the first two hold. A cell with a suspect or
wrong-model `model_identity` (or broken `verification_integrity`) is
**discarded data, not a model failure**: it is excluded from the role's score
and re-run, never scored as a fail for that model.
