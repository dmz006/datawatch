# Supervisor merge steps inventory (2026-11-15)

Read-only inventory of what the supervisor merge scripts do, mapped to the
daemon's built-in merge pipeline. The supervisor scripts live outside the
repository and were not read; their behaviour is taken from the feature
description. Anything not confirmed from the repository is marked
**(assumption)**.

Sources read: `scripts/check-merge-version.sh`, `scripts/check-docs-sync.sh`,
`scripts/check-no-unscoped-tmux.sh`, `internal/mergepipeline/default.go`,
`internal/mergepipeline/conflict_mechanical.go`,
`internal/mergepipeline/conflict_additive.go`, `internal/mergeshadow/gates.go`,
`docs/howto/merge-pipeline.md`.

## Daemon built-in pipeline (confirmed, `default.go`)

| Order | Step | Kind | On failure | Timeout |
|---|---|---|---|---|
| 1 | rebase-onto-main | custom | stop | 5 min |
| 2 | check-branch-versions | custom | stop | 2 min |
| 3 | check-internal-refs (`make check-internal-refs`, only if the target exists) | lint | stop | 10 min |
| 4 | merge-branches | ship | stop | 5 min |
| 5 | set-release-version | version bump | stop | 1 min |
| 6 | insert-changelog-heading | changelog | stop | 1 min |
| 7 | set-readme-release-line | version bump | stop | 1 min |
| 8 | commit-release-bump | ship | stop | 1 min |
| 9 | tag-release | tag | stop | 1 min |
| 10 | validate-release-bump | custom | stop | 1 min |
| 11 | push-release | ship | stop | 5 min |
| 12 | deploy-latest | deploy | continue | 10 min |

The merge shadow (`internal/mergeshadow/gates.go`) runs the make targets
`build`, `lint`, `test` in that order against a trial worktree. Each gate is
`pass`, `fail`, `timeout`, `skipped` (no such target) or `load_sensitive`
(failed, then passed when rerun alone).

## Supervisor script steps

| Step | Inputs | Outputs | Failure rule | Typical time | Repository command | Daemon built-in step |
|---|---|---|---|---|---|---|
| Trial merge | finished Automaton branch; current main | scratch worktree holding the merge result | conflict not settled mechanically → stop for the supervisor | ~1 min **(assumption)** | `git worktree add` + `git merge` of the branch | rebase-onto-main, merge-branches (5 min each) |
| Finish | trial worktree | lint, build, go test and PWA test results | any failure blocks the ship | 10–20 min **(assumption)** | `make lint`, `make build`, `make test`; PWA node tests **(assumption: exact command)** | merge shadow gates build/lint/test; check-internal-refs |
| Gates: docs-sync | trial worktree | pass/fail | non-zero exit blocks | seconds | `sh scripts/check-docs-sync.sh` | none |
| Gates: version | branch name, main | pass/fail | branch set its own version, or the two version files disagree → refuse | seconds | `bash scripts/check-merge-version.sh BRANCH` | check-branch-versions |
| Gates: tmux | scripts/, tests/, .datawatch/verify/ | pass/fail | unscoped `tmux kill-server` / `kill-session -a` → fail | seconds | `env -u TMUX -u TMUX_TMPDIR bash scripts/check-no-unscoped-tmux.sh` | none |
| Gates: gosec, gitleaks, registry, goreleaser | trial worktree | pass/fail per tool | any finding blocks **(assumption)** | 1–5 min **(assumption)** | gosec, gitleaks, registry check, `goreleaser check` **(assumption: exact commands)** | none |
| Release smoke | built binary / release bump | pass/fail | failure blocks the ship **(assumption)** | minutes **(assumption)** | release-bump validation **(assumption: exact script)** | validate-release-bump |
| Ship | merged main | version set to next patch above main, changelog heading, README release line, commit, tag, push, deploy | stop on failure; deploy failure does not undo the ship | ~5 min **(assumption)** | merge + version set + tag + push | set-release-version, insert-changelog-heading, set-readme-release-line, commit-release-bump, tag-release, push-release, deploy-latest |
| Chain | list of finished Automata | each run through the steps above in turn; mechanical conflicts resolved; shipped state recorded | stops at the first unresolved failure **(assumption)** | sum of the steps per Automaton | none (script outside the repository) | the whole pipeline per run, under the one-merge-per-repository lock; shipped record |

## Conflict auto-resolution rules per file class (confirmed)

From `conflict_mechanical.go`, `conflict_additive.go` and the how-to:

| Class | Files | Rule |
|---|---|---|
| code | any file not in another class | settled only when every hunk is additive on both sides; both sides kept |
| document | `.md` files and `docs/` (includes plan notes) | same additive rule as code |
| changelog | `CHANGELOG.md` | additive hunks keep both sides; a non-additive hunk is left unsettled |
| version_line | files in `version_files` | a hunk of only version lines keeps ours; anything else unsettled |
| readme_line | `README.md` | same as version_line |
| locale | top-level `*.json` under `internal/server/web/locales/` | flat key union; a key changed on both sides keeps ours, with a note |
| generated | `internal/docsindex/assets/`, `internal/server/web/docs/` | keep ours and rebuild from sources when a docs-index make target exists |

Binary files are never settled mechanically. Under the `stop_and_ask` policy
nothing is resolved: the merge is aborted and the operator is asked. Under
`mechanical_and_model` a model proposal is tried for what the rules leave.
How the supervisor script handles plan notes and docs beyond these rules is an
**(assumption)**: it is taken to apply the same additive rule.

## When a merge ships (confirmed)

- All checks must pass. A failed check never ships in any mode and is never asked about.
- Release approval mode per size: `auto` ships after passing checks; `ask` raises one "Ship this release?" question and ships only on yes; `manual` holds for a person.
- Defaults: patch `auto`, minor `auto`, major `ask` — a major release never ships quietly.
- The release gate holds back only the leaving steps (tag, push, deploy); merge, version bump, changelog and commit still land locally.
- The version given is the next patch above main (`check-merge-version.sh`).
