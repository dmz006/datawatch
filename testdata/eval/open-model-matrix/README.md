# Open-model matrix eval — fixtures

Test data only: nothing here is read by the daemon, and creating it changes no
default, daemon config or automaton-level LLM setting. It defines a second eval
grid over the open (pi-prompt/Ollama) models, reusing the Claude tiering eval's
task fixtures unchanged.

## Shape: 20 cells

5 roles × 4 task types = 20 cells, 1 rep each. Every role executes cells; the
`base` role (`qwen3.8:27b`, the confirmed-good baseline) additionally holds the
decompose/validate/guardrail roles in the scratch Automaton. The four other
roles are execution-only candidates — nothing in the vendor data supports them
in the non-executor roles (see `docs/plans/2026-10-15-bl449-open-model-matrix.md`).

| Role | LLM registry entry (placeholder) | Model | Node (placeholder) |
|---|---|---|---|
| base | `pi-node-b-gpu` | `qwen3.8:27b` | node-b |
| swift-1.5-qwen38-27b | `pi-node-a-gpu-s15` | `swift-1.5-qwen38-27b:latest` | node-a |
| qwen3-coder | `pi-node-a-gpu-q3c` | `qwen3-coder:30b` | node-a |
| qwen3-coder-next | `pi-node-b-gpu-q3cn` | `qwen3-coder-next:q4_K_M` | node-b |
| qwen3.5 | `pi-node-a-gpu-q35` | `qwen3.5:35b` | node-a |

## Thinking level (pi) per role

Each role carries a `thinking_level` field: executors pin `minimal`, while
non-executor roles leave it `null`, meaning pi's own default (`pi.thinking_level`,
allowed `off|minimal|low|medium|high`, see `internal/config/pi_thinking_level.go`).
The driver applies a role's level through the per-task override
`set_task_thinking_level` (POST `/api/autonomous/prds/<id>/set_task_thinking_level`),
only when that role sets one, and would set the PRD-level pi level
(`prd_thinking_level` at the top of `matrix.json`, null here) only if the
non-executor duties were pinned. Every cell result row records the level
actually used (`thinking_level`; null renders as `default` in the report
tables).

The LLM/node names above are placeholders. Your own registry entry and
compute-node names go in a local matrix that is never committed:

1. Copy `matrix.json` to `.datawatch/eval/open-model-matrix/matrix.local.json`
   (gitignored), or anywhere and point `DATAWATCH_EVAL_OPEN_MATRIX` at it.
2. Replace each role's `llm` and `node` with your names. Keep `role`,
   `model`, `thinking_level`, `task_types`, `fixtures_from` and `cell_root`
   unchanged.

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
effort anywhere; the pi-side reasoning-effort axis instead is the `thinking_level`
field described above, pinned per role and recorded per cell.

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
  (`cell_root` in `matrix.json`), e.g. `…/<run-id>/qwen3-coder-analysis`.
- Each cell is a fresh snapshot: `git archive <baseline_sha> | tar -x -C <cell>`,
  then `git init` + one baseline commit, so no other history is reachable.
- Seed the gitignored `internal/docsindex/assets/docs-bm25-index.json` with `{}`
  so `go build ./...` works, as the tiering README describes.
- The worker stays inside its cell dir, commits to the cell's own repo, and
  uses no memory recall, other branches or project history (the specs say so).

## Per-cell outcomes

Every cell records three independent outcomes:

1. **`model_identity`** — did the cell actually run on the role's model/node
   (e.g. pi/Ollama logs show `qwen3-coder:30b` on the qwen3-coder role's node),
   or is it suspect / wrong-model (fallback, different model loaded, wrong node)?
2. **`verification_integrity`** — did the verifier run cleanly against an
   untampered cell (cell isolated, verifier and fixtures unmodified, verifier
   exit status trustworthy)?
3. **`model_result`** — the verifier's pass/fail for the work itself.

`model_result` only counts when the first two hold. A cell with a suspect or
wrong-model `model_identity` (or broken `verification_integrity`) is
**discarded data, not a model failure**: it is excluded from the role's score
and re-run, never scored as a fail for that model.
