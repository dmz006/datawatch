# Gate report: cost-per-task display

Gate run for the per-task cost precision work. This task changes no source
files; the only repository change is this tracked report. The verify scripts
were run as-is from this worktree; failures are reported, not fixed.

Worktree state at run time: `git status --short` clean, `git diff HEAD
--name-only` empty (no staged or unstaged source changes in this worktree).

## Commands and results

### 1. `bash .datawatch/verify/cost-per-task-text-precision.sh`

- Exit status: `1`
- Confirmation: **fail** — the script aborted on its first content check with
  `text.go not changed`: the gate requires
  `internal/usagecalc/text.go` and `internal/usagecalc/text_test.go` to appear
  in the worktree's diff against `HEAD`, and this worktree has no such changes.

### 2. `bash .datawatch/verify/cost-per-task-pwa-cli-agree.sh`

- Exit status: `1`
- Confirmation: **fail** — the script aborted its change-file check with
  `no PWA/CLI file or test changed`: none of
  `internal/server/web/app.js`, `internal/server/web/app-usage-calculator-*.test.js`,
  `cmd/datawatch/cli_usage.go`, or `cmd/datawatch/cli_usage_test.go` appear in
  the worktree's diff against `HEAD`.

### 3. `bash .datawatch/verify/cost-per-task-release-notes.sh`

- Exit status: `141` (SIGPIPE)
- Confirmation: **fail** — the script died inside its version-extraction
  pipeline before printing any check verdict (last output line was its own
  `start` timestamp). The failing step is the
  `git show HEAD:internal/server/api.go | grep -m1 ... | sed ...` assignment:
  under the script's `set -euo pipefail`, the early exit of `grep -m1`
  breaks the pipe and the aborted pipeline status (141) trips `set -e`. No
  version-bump, CHANGELOG, or testing-tracker assertion was ever reached.

### 4. `env -u TMUX -u TMUX_TMPDIR go build -o .gate-scratch/datawatch ./cmd/datawatch/`

- Exit status: `0`
- Confirmation: **pass** — the CLI binary compiled cleanly to a scratch
  output path (`.gate-scratch/datawatch`, a non-tracked path under the
  project directory, removed after the run).

### 5. Cost-per-task 0.05 text-form confirmation

Rendered through the same text form the CLI prints
(`internal/usagecalc` `FormatCLI`), driven by a scratch renderer
(`.gate-scratch/cmd-gatecheck/main.go`, created for this run and removed
afterwards, never tracked) with input `RemainingWork{QueueTasks: 4,
CostPerTask: 0.05}`:

- Command: `env -u TMUX -u TMUX_TMPDIR go run ./.gate-scratch/cmd-gatecheck`
- Exit status: `0`
- Confirmation: **pass** — the text form printed the configured cost at its
  typed value:

  > remaining work 0.2 points (estimate: 4 queued tasks at 0.05 points each)

  i.e. a configured cost per task of 0.05 prints as `0.05`, not `0.1`.

Corroborating run (same text/comm rendering path, both forms asserted to
contain the `0.05` figure):

- Command: `env -u TMUX -u TMUX_TMPDIR go test ./internal/usagecalc/ -run TestWorkLineCostPerTaskPrecision -v -count=1`
- Exit status: `0`
- Confirmation: **pass** — all four table cases (including the `0.05` case)
  passed.

## Notes

- The precision behavior itself is verified present and correct in this tree
  (items 4–5); the gate failures in items 1–3 are because this worktree
  carries the change only as base-tree content, not as a diff against `HEAD`,
  and because item 3's script has a fragile `git show | grep -m1 | sed`
  pipeline that dies under `pipefail` before it can evaluate.

result: fail
