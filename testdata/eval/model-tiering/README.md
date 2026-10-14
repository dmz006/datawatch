# Model/effort tiering eval — fixtures

Fixed, disposable fixtures for evaluating which Claude model + effort level
datawatch should use for each LLM touchpoint. This directory is **test data
only**: nothing here is read by the daemon, and creating or running it changes
no default, no daemon config and no automaton-level LLM setting.

The fixtures are adapted from this repo's own real past task specs (the
session-tracker OOM fix work), so the eval measures real dev-task shapes, not
toy tasks.

## Touchpoints covered

| Touchpoint | What varies | Fixture here | Cells |
|---|---|---|---|
| Execution | worker model/effort for one task | `specs/`, `verify/`, `review/` | 4 task types × 13 tiers = 52 |
| Decompose / planning | model/effort used by `autonomous_prd_decompose` | `decompose/` | 1 spec × 13 tiers = 13 (decompose only, nothing executed) |
| Verification | the daemon's quality-gate verifier | **none — by design** | see below |

**Verification tier is not a fixture.** There is no per-task or per-automaton
override for the verifier today; exercising it means temporarily changing the
GLOBAL `autonomous_config.verification_backend` / `verification_effort`. That
must only ever happen inside a capture/restore-guarded live run, so it lives
entirely in the driver task and the live-run task, not here. `matrix.json` has
`"verification_tiers": null` to make this explicit.

## Files

| Path | What it is |
|---|---|
| `matrix.json` | Task types, execution tiers, decompose tiers, valid models/efforts, reps, observed default. `tiers` is a legacy alias of `execution_tiers` kept for the existing fixture verifier. |
| `specs/{analysis,scripting,docs,review}.md` | Worker-facing cell specs. Self-contained; each tells the worker to stay inside its cell dir, commit to the cell's own repo, and not use memory recall, other branches or project history. |
| `verify/{analysis,scripting,docs,review}.sh` | Per-cell verifiers: `verify/<type>.sh <cell-dir>`, exit 0 only if the cell meets its spec. |
| `verify/hidden/tracker_eval_hidden_test.go` | Hidden behavioural Go test that `analysis.sh` drops into the cell temporarily (see below). Under `testdata/`, so the go tool never builds it in place. |
| `review/candidate-pass.diff` | `git diff d2f30df2 54954c5f -- internal/session/` — the real accepted fix, verbatim. |
| `review/candidate-fail.diff` | The same fix with one deliberate, spec-violating defect (see below). |
| `review/expected.json` | `{"candidate-pass.diff":"pass","candidate-fail.diff":"fail"}` |
| `review/cell-names.json` | Neutral in-cell names for the two diffs (see "Answer leakage"). |
| `decompose/ground-truth-spec.md` | The real top-level spec of automaton `6f3e1f5d`, verbatim (plus a trailing newline). |
| `decompose/ground-truth-structure.json` | The real decompose's shape: 2 stories, 5 tasks, dependency edges, file lists, coverage and out-of-scope rules. No spec bodies. |
| `decompose/rubric.md` | Objective, script-checkable scoring of a decompose cell against the ground truth (max 100). |
| `rates.json` | Per-model USD per million tokens (input, output, cache write, cache read) for opus, sonnet, haiku, fable. |
| `transcript-sample.jsonl` + `transcript-sample.expected.json` | Tiny synthetic Claude Code transcript and its hand-computed totals, for the driver's offline self-test. |

## Task types and baselines

| id | Baseline | Source task (real) | Shape |
|---|---|---|---|
| analysis | `d2f30df2` (parent of the real fix) | "Exclude output.log from tracker git and set safe gc config" (automaton 6f3e1f5d, story 1 task 1) | Read `tracker.go` + `manager.go` callers end to end, write a concurrency-safe fix + new `tracker_test.go` |
| scripting | `54954c5f` | "Reproduction harness for tracker git growth" (story 1 task 2) | Mostly mechanical bash against a detailed CLI spec. All hardcoded absolute system-binary paths removed; tools by name / `command -v` |
| docs | `54954c5f` | Docs-only slice of "Release, deploy, and observe the tracker fix" (story 2) | CHANGELOG Unreleased entry + `docs/session-tracking.md` row and section. No version bump, no code, no tracker IDs |
| review | `54954c5f` | Mirrors the daemon's verifier role | Given the analysis spec + two diffs, write `review-verdict.json` |

## Cell isolation

Each cell is a fresh snapshot: `git archive <baseline_sha> | tar -x -C <cell>`,
then `git init` + one baseline commit. No other history is reachable from the
cell, so a worker cannot `git log`/`git show` its way to the real answer.

`git archive` leaves out the gitignored, generated embed
`internal/docsindex/assets/docs-bm25-index.json`, and without it
`go build ./...` fails. Cell setup should seed the same `{}` placeholder the
Makefile uses (`mkdir -p internal/docsindex/assets && echo '{}' > …`) before
handing the cell to a worker. `verify/analysis.sh` seeds it too if it's
missing. It is gitignored, so it never appears in a cell's diff.
Specs also tell the worker not to use memory recall, which would otherwise
surface notes about the real fix.

## Answer leakage guards

- Specs must not contain the reference fix SHA or the backlog ID of the real
  fix; the fixture verifier greps `specs/*.md` for both.
- **Review diff names.** The fixture files are named `candidate-pass.diff` /
  `candidate-fail.diff` (required names), but those names give away the
  verdict. The driver should copy them into the cell under the neutral names in
  `review/cell-names.json` (`candidate-a.diff` = fail, `candidate-b.diff` =
  pass; `matrix.json` repeats this under the review task's `cell_inputs`). The
  review spec only says "two files matching `candidate-*.diff`, key the verdict
  by exact file name", and `verify/review.sh` maps either naming back to
  `expected.json`, so both work. With the original names the review cell is
  effectively answered, so don't use them in a live run.
- `candidate-fail.diff` was produced by really applying the pass diff to a
  baseline snapshot, editing it, and re-diffing with the same 8-character
  index abbreviation. Hunk headers and index hashes are genuine, so they don't
  give it away either.

### Injected defect in `candidate-fail.diff`

Two lines removed from the real fix, everything else (tests, file names,
comments) identical:

1. `{"gc.auto", "0"},` dropped from `ensureRepoHygiene`'s config list, so
   implicit auto-gc stays enabled. That is the root cause the spec exists to
   remove.
2. The lazy `t.hygieneOnce.Do(t.ensureRepoHygiene)` call at the top of
   `commitAll` dropped, so trackers built by `ResumeTracker` (every existing
   long-lived repo) never get the exclude / untrack / gc fix.

Both break explicit spec requirements (1a and 2). The unchanged tests
(`TestNewTracker_SetsSafeGCConfig`, `TestResumeTracker_UntracksLegacyOutputLog`)
would fail if run, so a reviewer that actually builds and tests the candidate
can show the failure as well as reason about it.

## Verifiers

All take the cell dir as `$1`, use `set -euo pipefail` and timestamped lines,
and print `RESULT: PASS` on success.

- `analysis.sh`: gofmt, `go build ./...`, `go vet` + `go test ./internal/session/`,
  the original `gc.auto` / `output.log` test-presence assertions, then a
  **hidden behavioural test** (`verify/hidden/…`) copied into the cell as
  `internal/session/zz_eval_hidden_test.go` for one run and removed again. A
  cell can't pass by writing weak tests of its own. Set `EVAL_GOLANGCI=1` to also
  run golangci-lint on the package (off by default, for speed).
- `scripting.sh`: script exists and is executable, `bash -n`, shellcheck if
  installed, no literal system-binary directory paths (line-1 shebang only),
  refuses a workdir under `$HOME/.datawatch` (tested with a fake `HOME`), runs
  at `--commits 60 --chunk-kb 256` in a `mktemp -d` dir and must print
  `RESULT: PASS`.
- `docs.sh`: changes vs the cell's root commit (committed, staged, unstaged,
  untracked) are only `CHANGELOG.md` + `docs/`. Both `CHANGELOG.md` and
  `docs/session-tracking.md` must be touched. No `BL[0-9]+` in added lines,
  both `var Version` lines unchanged, and no new version heading. The Unreleased
  section must mention `output.log` and gc, the `output.log` row must say it is
  not committed, and a "Repository size" section must exist.
- `review.sh`: `review-verdict.json` is valid JSON. Each candidate's verdict
  (string or `{verdict, reasons}`) must equal `review/expected.json`, which is
  resolved relative to the script's own location, not the cwd.

Self-check (`.datawatch/verify/model-tiering-fixtures.sh`): every verifier
FAILS on an untouched baseline snapshot; `analysis.sh` PASSES on a `54954c5f`
snapshot; `review.sh` PASSES on the ground-truth verdict.

## Tier grid

`valid_models` / `valid_efforts` come from datawatch MCP `claude_models` /
`claude_efforts`, read 2026-10-13: models `opus`, `sonnet`, `haiku`, `fable`
(`claude-opus-5-5`, `claude-sonnet-5-5`, `claude-haiku-5-5`,
`claude-fable-5-1`), efforts `low`, `medium`, `high`, `xhigh`, `max`.

- **Execution:** 4 models × {low, medium, high} = 12 tiers, plus one
  `default` tier = **13**. Across 4 task types that is 52 cells at `reps: 1`.
- **Decompose:** the same 13 tiers, one cell each = **13** cheap,
  decompose-only cells.
- **`xhigh` / `max` excluded on purpose** to keep the live run practical
  (12 → 20 tiers would make 80 execution cells). They are valid values. If
  `high` turns out to be the best tier for some task type, a follow-up run can
  add `xhigh`/`max` for that model only.

### Today's default (`observed_default`)

Read 2026-10-13 via `llm_get claude-code` + `autonomous_config_get`: the
claude-code backend launches with `--model opus` and **no `--effort` flag**.
datawatch's `default_effort: "normal"` is its own quick/normal/thorough hint
and is not passed to claude, so the claude CLI's built-in default effort
applies. Planning, execution and verification backends are all `claude-code`.

That doesn't match any of the 12 grid tiers exactly, because "unset" isn't the
same as an explicit `medium`/`high`, and the CLI default can change between
CLI releases. **Decision: add a 13th `default` tier** (`model: opus`,
`effort: null`, meaning the driver passes no effort flag) to both grids, so
the results can say directly how today's behaviour compares with each explicit
tier.

## Costing: why a separate `rates.json`

datawatch's own cost table (`cost_rates`, `internal/session/cost.go`) has one
flat `claude-code` rate for every Claude model (a Sonnet-ballpark
$3/$15 per M). `internal/session/claude_usage.go` also deliberately leaves
cache tokens out. So it can't tell tiers apart. Model choice is the variable
under test, and cache reads dominate long agentic sessions. The eval therefore
prices each transcript itself, per model, with `rates.json`:

| model | input | output | cache write (5m) | cache read |
|---|---|---|---|---|
| opus (`claude-opus-5-5`) | 4.00 | 20.00 | 5.00 | 0.20 |
| sonnet (`claude-sonnet-5-5`) | 2.00 | 10.00 | 2.50 | 0.20 |
| haiku (`claude-haiku-5-5`) | 0.10 | 0.50 | 0.125 | 0.01 |
| fable (`claude-fable-5-1`) | 10.00 | 50.00 | 12.50 | 0.25 |

USD per million tokens, as of 2026-10-06, from Anthropic's pricing page as
cached in the Claude API reference skill. Cache write is the 5-minute TTL rate
(1.25× input). Haiku's rates apply to prompts of 100K tokens or fewer, and its
cache read assumes the standard 0.1× multiplier. Map a transcript's
`message.model` to a row with the `model_match` prefixes.

### Transcript self-test

`transcript-sample.jsonl` has 1 user line and 4 assistant lines: 3 from
`claude-opus-5-5`, 1 sidechain line from `claude-haiku-5-5`. Hand-computed
expected totals are in `transcript-sample.expected.json`:

- opus: in 1280, out 1650, cache write 9500, cache read 16000.
  Cost = (1280×4 + 1650×20 + 9500×5 + 16000×0.20) / 1e6 = **$0.08882**.
- haiku: in 400, out 120. Cost = (400×0.10 + 120×0.50) / 1e6 = **$0.0001**.
- total **$0.08892**.

## Provenance notes

- Ground truth read via `autonomous_prd_get 6f3e1f5d` on 2026-10-13. The real
  decompose has **5** tasks (2 + 3), not the 4 that earlier planning notes
  said; the fixture records what is actually there.
- The real scripting task was blocked by datawatch's scope lint because its
  spec named an absolute `time` binary path. The adapted spec here requires
  `$PATH` resolution, and `scripting.sh` enforces it.
