# Parity issue draft — shadow merge plan (Phase 4 of the autonomy handover, shadow mode)

**Not filed, and not yet fileable.** — this is the tracked draft; the issue on the
app tracker is opened when the operator files it. Writing this draft changes nothing
on any host, and filing it never runs a hosting CLI or creates anything on a
tracker. This draft is a file write only.

**Parent work:** "Part of finishing review and merge by the daemon (Phase 4 of the
autonomy handover plan)" — the step that added the shadow merge plan: one record per
finished Automaton describing what its merge would do, built by the new package
`internal/mergeshadow` (the plan value and its persistence in `plan.go` and
`plan_store.go`, the throwaway-worktree trial merge in `trial.go`, the changelog
entry reader in `changelog.go`, and their tests `plan_store_test.go`, `trial_test.go`
and `changelog_test.go`). The operator-visible record of this step is the
"Shadow merge plan" entry in `docs/datawatch-definitions.md`, the "The shadow merge
plan" section of `docs/howto/merge-pipeline.md`, the feature-matrix row in
`docs/README.md`, and the operations note in `docs/operations.md` — documentation
only, no product surface.

## Why this draft exists but names no client work yet

The Mobile-Parity Rule asks for a datawatch-app issue or comment for any
operator-visible change — a PWA layout or behavior change, an API contract change,
an affordance, or a default a client mirrors. This step makes none of those: the
plan is a daemon-internal record with **no operator-facing reads at all** — no REST
route, no MCP tool, no CLI verb, no comm channel verb, no PWA card and no new
settings until the rest of Phase 4 lands them. There is therefore nothing for the
Android and iPhone clients to render, parse or mirror, and per the rule's own
"no issue is needed" clause (invisible to the operator, no API contract change)
there is no app issue to file today. This draft is the standing record of that
judgment, and of what the parity obligation will be the moment the obligation
triggers, so the later Phase 4 step does not have to invent it under time pressure.

## Affected area

The daemon's merge side, in the shadow period of Phase 4 of the autonomy handover
(`docs/plans/2026-11-03-autonomy-handover-plan.md`): the daemon plans a finished
Automaton's merge and records the plan without touching the repository — the
release size (`patch`, `minor`, `major` or `none`), the version the bump step would
set, the changelog heading and body, the changelog source (the entry task id, or
`missing`), the README line, the registry row, the parity-issue flag, each conflict
with its file class, resolution and resolver, each gate with its status and
duration, and each finding with a code and whether it blocks. Client-relevant
consequence today: **none** — the plan is read only by the daemon itself (the
supervisor's comparison reads the files), and no client ever sees it.

## Current behavior

One plan per Automaton, written atomically to `merge-plans/<id>.json` under the
daemon's data directory; reads list the newest first; per-project retention prunes
to the newest N (N ≥ 1). Three invariants hold by construction, and each is the
exact wording a future client contract must not break: a plan is never applied —
every stored plan carries `applied: false` and the store refuses a plan marked
applied, so a plan is a record, not a queue; a plan never touches the project —
the trial merge that finds the conflicts runs in a throwaway worktree outside the
project tree, uncommitted (`--no-commit --no-ff`), with the scratch tree removed on
every exit path, no ref created or moved and nothing pushed; and a plan never
invents — the changelog text comes only from the Automaton's own changelog entry
task when its output has the exact `## [version] - YYYY-MM-DD` heading with a
non-empty body, and otherwise the plan carries an empty heading and body with the
source `missing`. On the client side the current behavior is the pre-Phase-4
behavior: the merge pipeline features (`GET/PUT/DELETE/POST /api/autonomous/merge-pipeline`,
the conflict policy and model settings, see `docs/plans/parity-issues/merge-pipeline.md`)
are already mirrored by Android and iPhone, and this step adds nothing the clients
should know.

## Expected behavior

No new client behavior is expected while the plan stays internal. The obligation
that this draft holds open fires when the rest of Phase 4 — the release approval
modes per release size (`automatic`, `ask` or `manual`), the daemon's own push and
tag under a release path, and the conflict-resolution confirmation counter — exposes
any of it: a read route for the plan (or the approval state that rides on a merge
run), a new config key under `autonomous.merge_pipeline.*` or a new approval-mode
key, a new MCP/CLI/comm verb, or a PWA card. At that point the Android and iPhone
clients are expected to receive the same read (the plan's fields above, with
`applied` always `false` while shadow mode holds), to expose the same settings as
each platform's idiom, and to carry the same locale behavior — every string saying
Automaton/Automata in all five locales, with translations through the Compose
Multiplatform pipeline. Capability parity will then be required; implementation
parity is not, per the rule. Until the shadow comparison meets its evidence bar,
the merge stays with the supervisor and no client is expected to act on a plan at
all — a client that does would be ahead of the server's own policy.

## Suggested next step

When the Phase 4 step that adds the first operator-facing surface for the plan or
the approval modes lands: file this draft (reworded to name that step's concrete
routes, keys, verbs and card), after first checking the app tracker for an open
issue (`gh issue list -R dmz006/datawatch-app --state all --search 'shadow merge plan'`),
then replace the status line at the top of this file with the issue reference.
Every one of the later Phase 4 surfaces that changes an operator-visible contract —
each new REST route, each new config key, each string that enters the locale
bundles — must re-check this file and fold its contract in before the merge
advances past shadow mode; the moment the daemon starts applying a plan (the first
legitimate `applied: true`), the parity contract must say exactly what a client
shows while a merge run is pending approval in `ask` mode and how the operator
answers, because that interaction does not exist in the clients at all today.

## Filing record

Not filed. Nothing to check or create on a tracker: the step this draft documents
touched no API contract, no string, no default and no client-visible surface —
which is why the file is a draft and a standing record, not a live issue.
