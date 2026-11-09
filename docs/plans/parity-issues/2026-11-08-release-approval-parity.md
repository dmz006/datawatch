# [sync] Release approval: resolved and validated, but no merge path honours it yet (Phase 4 of the autonomy handover plan)

**Filed 2026-11-09 as dmz006/datawatch-app#419, one consolidated issue for all the release approval drafts** (shipped in v9.116.0).

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Story** (22da0001, "Part of finishing review and merge by the daemon"): the
release approval modes are now a setting with one mode per size of release —
`autonomous.merge_pipeline.release_approval.{patch|minor|major}`, each `auto`,
`ask` or `manual`, with per-project overrides — carrying their resolution and
validation, tested. The step the plan runs beside the supervisor's manual
merge, which would honour that setting, is not in place yet.

## Affected area

The gap sits between the config layer and the daemon's own review-and-merge
path. What this story lands, in `internal/config`: the `ReleaseApprovalModes`
block (one key per release size) on the global `autonomous.merge_pipeline`
block and on each project override, the resolution chain — a project's value,
then the global value, then the built-in defaults (patch `auto`, minor `auto`,
major `ask`) with an unknown or unread size resolving to `ask` so nothing ships
quietly — and the validation that refuses any other mode with the key named
(`ValidateReleaseApprovalMode`, wired into `Validate()` for the global and
every project). All of it is table-tested: defaults, global and project
precedence, partial-override fall-through, unknown-size `ask`, deep clone,
and accept/refuse of each mode.

What it does **not** yet touch is anything that would act on the resolved
mode. The daemon's merge pipeline (`internal/mergepipeline` — the checks,
merge, version bump, changelog, tag, push and deploy steps) contains no
reference to release approval at all; the `EffectiveReleaseApproval` and
`EffectiveReleaseApprovalModes` accessors have no callers outside
`internal/config`; and the read surfaces of the pipeline (the
`autonomous_merge_pipeline_get` view and its mirrors) do not report the
resolved mode per size. Operator guidance for the setting lives in
[docs/howto/merge-pipeline.md](../../howto/merge-pipeline.md) (*Choose the
release approval*), the `release_approval` row of the merge pipeline block in
[docs/config-reference.yaml](../../config-reference.yaml), the
*Release approval* entry of
[docs/datawatch-definitions.md](../../datawatch-definitions.md), and the
merge pipeline section of
[docs/operations.md](../../operations.md); the `## [Unreleased]` changelog
entry records this exact scope.

## Current behavior

The setting exists, round-trips, and resolves — it does not act. An operator
can set `release_approval.major` to `ask`, `auto` or `manual` in YAML, via
`datawatch config set`, `PUT /api/config`, MCP `config_set` or the comm
channel `configure` verb; an unknown mode is refused with the key named rather
than stored silently; and the resolution chain answers correctly for every
size and every project. That is the surface of the parity this story closes:
the approval decision now has a value, a precedence, a default, and a test
that none of those can be wrong or missing.

The behavior the value would change, however, is still exactly what it was
before: who approves a release is convention, not configuration. The
supervisor reviews and merges by hand and leaves major releases to the
operator by rule; `auto` does not ship anything, `ask` does not ask anyone,
and `manual` does not hand the operator anything extra. The `autonomous_merge_pipeline_get`
view reports the pipeline, its source, the conflict policy and model, and any
per-project overrides — but not the approval mode in effect for a project and
size, so an operator asking "who approves my major release, and for which
project" gets the raw setting, not the resolved answer. The howto is honest
about this: it says the setting "is the value the daemon's merge path will
honour" and that, until the step lands, "a release is made exactly as
before."

The remaining work, per the plan's own scope note, is the daemon's own
review-and-merge step — the one that runs in shadow beside the supervisor's
manual merge and records every difference as a defect — and its
push-and-tag step, both still to come in later steps of the same Phase 4 work.

## Expected behavior

Once the Phase 4 steps are in, the daemon's review-and-merge path resolves the
approval mode for the release it is about to make — the same
`EffectiveReleaseApproval(project, size)` this story already tested — and
honours it: `auto` ships that size without waiting for anyone; `ask` asks the
operator before the release ships and records the answer; `manual` leaves the
finished, reviewed release for the operator to ship, and the daemon stops at
that boundary. A release whose size cannot be read is never shipped quietly.
The shadow step makes this observable before it is binding: the daemon's
merge runs beside the supervisor's manual merge, and every place the setting
would have decided differently from what actually happened is recorded as a
defect with the evidence (project, resolved mode, size, what was done), so
when enforcement switches on, it switches on with a record of how often
setting and practice already disagreed. Once enforcement is on, the pipeline
read surfaces report the resolved mode per size alongside the policy and
model they already report, so the effective "who approves this" answer is one
read away instead of a precedence puzzle.

## Suggested next step

Work the plan's own remaining Phase 4 steps in order, each as its own story:
first the shadow step — wire `EffectiveReleaseApproval` into the daemon's
review-and-merge path in record-only mode, running it beside the supervisor's
manual merge and logging every difference (resolved mode vs. what was
actually done) as a defect record with project, size and evidence — then the
enforcement step that lets `auto`, `ask` and `manual` actually decide, and
finally the push-and-tag step the shipped mode gates. As part of wiring the
setting into the path, add the resolved per-size mode to the
`autonomous_merge_pipeline_get` view and its mirrors, so the read surfaces
answer "who approves this" the same way `ResolveConflictPolicy` answers
"who resolves conflicts." Until enforcement is on, the operator guidance
stays as written: the value is settable, validated and resolvable, and the
way releases are made is unchanged.
