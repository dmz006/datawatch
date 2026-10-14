# Decompose-tier scoring rubric

A decompose-tier cell runs `autonomous_prd_decompose` (only — no task is ever
executed) on a scratch PRD whose spec is `ground-truth-spec.md` verbatim, with
the cell's tier as the PRD's decomposition model/effort. The cell's output is
the resulting story/task tree as returned by `autonomous_prd_get`. It is scored
against `ground-truth-structure.json` by a script, using only the fields
below. No check needs a human or an LLM judge.

## Input normalisation

From the cell's `autonomous_prd_get` response, build:

- `stories[]` — in returned order; each has `title`, `depends_on` (story ids).
- `tasks[]` — each has `title`, `spec`, `files` (array, may be empty),
  `depends_on` (task ids), and its parent story.
- Ids are mapped to positional keys (`S1`, `S1.T1`, …) the same way the
  ground truth does. Ids themselves are never compared.
- File paths are compared as exact repo-relative strings.

## Checks (each is pass/fail, worth the listed points; max 100)

| # | Check | Pass condition | Points |
|---|-------|----------------|--------|
| C1 | Story count | `abs(len(stories) - 2) <= 1` | 10 |
| C2 | Task count | `abs(len(tasks) - 5) <= 1` | 10 |
| C3 | Every story has tasks | every story has `>= 1` task | 5 |
| C4 | Dependencies resolve | every `depends_on` id names an existing story (for stories) / task (for tasks) | 10 |
| C5 | Dependencies acyclic | story graph and task graph (story edges expanded to all tasks of the depended-on story) are DAGs | 10 |
| C6 | Coverage: code-fix | some task's `files` contains `internal/session/tracker.go` | 10 |
| C7 | Coverage: regression-test | some task's `files` contains a path matching `^internal/session/.*_test\.go$` | 5 |
| C8 | Coverage: repro | some task's `title` or `spec` matches `/repro\|reproduc/i` | 5 |
| C9 | Coverage: remediation | some task's `title` or `spec` matches `/remediat\|shrink/i` and also `/bloated\|ab22\|9245\|ddeb/i` | 5 |
| C10 | Coverage: release | some task's `files` contains all of `cmd/datawatch/main.go`, `internal/server/api.go`, `CHANGELOG.md` | 5 |
| C11 | Coverage: backlog-close | some task's `files` contains `docs/plans/README.md` | 5 |
| C12 | Ordering: code before release | every C10 task transitively depends (task edges + expanded story edges) on at least one C6 task | 10 |
| C13 | Out of scope: max_parallel_tasks | no task `spec` matches `/max_parallel_tasks/` unless every match line also matches `/do not\|don't\|leave\|unchanged\|not change/i`; no task `files` entry matches `datawatch\.ya?ml$\|config\.ya?ml$` | 5 |
| C14 | Out of scope: re-architecture | no task `files` entry under `internal/session/` other than `tracker.go` and `*_test.go` | 3 |
| C15 | Out of scope: unrelated files | every task `files` entry is in ground-truth `all_files`, or matches `^(scripts\|docs)/` or `^\.datawatch/verify/` | 2 |

`score = sum(points of passed checks)`. A cell also records the raw counts
(stories, tasks, edges) and the list of failed check ids so the results doc
can show *why* a tier lost points, not just the number.

## Not scored (recorded only)

- Wall-clock decompose time and token cost (from the planning session
  transcript, priced with `../rates.json`) — reported next to the score, not
  folded into it.
- Exact titles, and the split of work between tasks — legitimately vary
  between good decompositions.

## Ground-truth caveat

The real decompose produced **5** tasks (2 in story 1, 3 in story 2), not 4 as
earlier planning notes said; `ground-truth-structure.json` records the real
record as read on 2026-10-13. The ±1 tolerance in C1/C2 is deliberate: the
real decompose is one good answer, not the only one.
