# CodeQL alert gate — trigger narrowing

## Date

2026-11-07

## Version

The version is the next patch above main, set at merge time by the merge
helper. No number is written here.

## Status

Planned for the merge back to `main`: nothing here is on `main` yet. The
trigger edits themselves are already in place on this branch — commits 81db2ca54
and 99a71e45f, merged in as 50c9c7e30 — so what is still pending is the merge
to `main`, not the edits.

Phase status:

- Phase 1 — the trigger edits, the test and the Makefile: **done**, on this branch.
- Phase 2 — the two how-to paragraphs: **done**, this task, on this branch.
- Phase 3 — merge to `main`, then the operator's live check: **planned**.

## Scope

- `.github/workflows/codeql.yaml` — the trigger blocks only (see Conflicts).
- `tests/test-codeql-trigger-listing.sh` — the trigger-listing test.
- `Makefile` — the target that runs that test.
- `docs/howto/codeql-local-scan.md` — one new paragraph in
  "What it is and why".
- `docs/howto/automation-repo-ci-watch.md` — the same paragraph in
  "What the watch sees".

## Operator words

Quoted verbatim, in the order they were given:

> "I approve the change but I'm questioning the way it is working, it is doing
> a lot is it suppose to be continuous or on release, is it designed to match
> what we are doing and what it is doing"

> "Those changes are good, make them"

## Decisions

The trigger edits this section explains are already on this branch, so the
decisions justify what is there rather than propose anything new.

**The push trigger.** Stays `branches: [main]`, gains `paths-ignore` for
`docs/**`, `CHANGELOG.md` and `**/*.md`. Why: a push that changes only
documents cannot add a code-scanning alert, so spending a gate run on it buys
nothing while consuming the concurrency slot for real runs.

**The pull_request trigger.** Stays `branches: [main]`, gains a `paths` list:
`**/*.go`, `go.mod`, `go.sum`, `channel/**`, `internal/server/web/**`,
`**/*.js`, `**/*.ts`, `**/*.mjs`, `**/*.py`, `.github/workflows/**`,
`scripts/**`. Why: the gate can only care about a change that could add an
alert, and CodeQL analyzes Go, JavaScript, TypeScript, Python and actions.
The JavaScript, TypeScript, module (`**/*.mjs`) and Python globs are there
because CodeQL analyzes those languages and a change to them must still run
the gate. The workflow and script globs stay because changes there can change
the code the gate reads. A pull request that changes none of the code,
dependency, workflow or script files starts no run.

**Dependency bumps still run.** `go.mod` and `go.sum` are in the `paths`
list, so a change to them still starts a run. A dependabot pull request that
bumps a Go dependency therefore still runs the gate. This differs from the
operator's remark that dependabot changes are covered by dependency review —
that is true of the review, not of this gate — and it needs the operator's
review before the merge, recorded here as an open question.

**Everything else is unchanged.** The schedule (Monday at 04:17 UTC, weekly),
the manual trigger (`workflow_dispatch`), the concurrency group, the
permissions block and the jobs block are untouched by this change.

## Protected path

The change edits `.github/workflows/codeql.yaml`, which is on the default
protected path list, so the daemon holds the edit for an operator decision,
and that question is recorded here: approve the protected path edit, or send
the change some other way. It is decided on this branch: the operator approved
the change with the words in Operator words above, and the edit is already on
this branch. What the merge to `main` still carries is that same protected
path edit, unmodified and standing under that approval.

## Branch protection and required check

There is no in-repo view to take: branch protection is a GitHub-side setting
this repository cannot show, and its state is known only from the GitHub side.
`docs/plans/historical-plans/2026-10-26-response-to-issues-207-210.md`
records main as unprotected, but that record is unverified.

The consequence that matters: a required check that never runs on a skipped
trigger can block merging. If the operator has made the job named "Open high
or critical alerts" a required check for pull requests, then a documents-only
or otherwise unrelated pull request — one that now starts no CodeQL run —
would wait for a check that never appears, forever.

The options are:

1. Confirm the check is not required. Recommended; no change needed.
2. If it is required, add a second workflow that reports the same check name
   on the complementary paths and passes when nothing changed. That is a
   second protected path change needing its own decision.

A push is not gated by merge requirements, so the push trigger has no such
consequence.

## Conflicts

Another open change touches the gate logic of the same workflow file. Rebase
on it and keep this change to the trigger blocks only — nothing in the jobs
block.

## After merge

The operator's live check on GitHub, which no task can run:

- a documents-only push to main starts no CodeQL run;
- a pull request that changes a Go file starts one;
- the scheduled run and a manual run still start.

## Parity surface

N/A on every surface: `REST`, `MCP`, `CLI`, `comm channel`, `YAML/config`,
`PWA`, `Android`, `iPhone/iOS` are untouched. This change is a GitHub Actions
trigger plus operator-facing documentation; it changes no API, no config
field and no client surface.
