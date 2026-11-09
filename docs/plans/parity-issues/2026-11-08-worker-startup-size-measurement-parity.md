# [parity] worker start-up size is measured and reported per profile and role (datawatch-app follow-up)

**Filed 2026-11-09 as dmz006/datawatch-app#416** (shipped in v9.115.0).

**Not filed yet.** This is the tracked draft. The GitHub issue is opened on the
`dmz006/datawatch-app` repository when the operator files it. Filing the issue does
not change a line of this repo. Draft written 2026-11-08 against the work for "Make
every worker session start small, with context and tools chosen for its job,
configurable for every datawatch user." (PRD `24230001`), specifically the slice that
measures how large a worker's starting context is and reports that size per worker
profile and per role: the attempt-level estimate (`internal/autonomous/startup_tokens.go`),
the cost report's start-up tables (`internal/costreport/report.go`), the stats block
(`internal/stats/worker_startup_stats.go`) and the usage advice start-up line
(`internal/server/usage_calculator_advice.go`, with `internal/mcp/usage_calculator.go`
exposing the same advice to MCP clients).

This draft is a sibling of `2026-11-08-worker-small-start-parity.md`, which covers the
pinned MCP set and the deterministic starting-context builder, and of
`2026-11-08-worker-session-profiles-parity.md`, which covers the profile settings
surface. This draft covers the measurement and read-back that tells an operator whether
the small start actually happened and how big it was.

Parent release: the datawatch release that ships worker session profiles, the feature
currently under `[Unreleased]` in `CHANGELOG.md`. Per the start-size work in that
release, the start-up figures are new read-only output on existing surfaces; no new
setting is introduced by this slice.

## Affected area

Three surfaces read the same start-up figures, and all of them are daemon-side today:

- **The attempt record.** Every attempt carries a start-up token size and a source. A
  launch estimates the size from the generated starting context at four bytes per
  token, rounded up, and marks the source `estimated`. A value the worker or its runner
  reports at runtime replaces it with source `reported`. An attempt with zero tokens
  (records written before sizes were captured) or an unrecognized source counts as
  no data.
- **The cost report and the stats block.** The cost report aggregates attempts into a
  per-profile table and a per-role table. Each row carries the attempt count, the mean
  start-up size over attempts that have one, and the counts of estimated, reported and
  no-data attempts. The stats snapshot's `worker_startup` block (`GET /api/stats`, the
  `get_stats` MCP tool and `datawatch stats`) reports only the averages, keyed by profile
  and by role, and is built from the same aggregation, so the two cannot disagree.
- **The usage advice start-up line.** The usage advice names the worker profile with
  the largest average start-up size and suggests a lighter profile for the role. It is
  advice only: the line reads nothing and changes no setting, and it is emitted only
  when at least one profile has an average.

From the operator's point of view, the affected question is simple: after a worker
session starts small, can the operator see how small, for which profile and role, and
can they tell an estimate from a figure the runner actually reported.

## Current behavior

On the daemon, today:

- The start-up size is an estimate by default. The estimate is the size of the
  generated starting context, not a count of what the model actually read, so two
  attempts with identical starting text estimate to identical sizes regardless of what
  the runner did afterwards. A runner-reported figure is recorded only when the runner
  supplies one, and the source label keeps the two apart in every table.
- The per-profile and per-role averages exclude zero-token attempts. A key whose
  attempts all have no size is omitted from the stats block rather than reported as
  zero, and it shows in the cost report with an attempt count and a no-data count
  instead. An operator therefore sees a profile with attempts but no average as a
  visible gap, not as a small start.
- The stats block has no per-attempt detail. It gives one number per profile and one per
  role. Which attempts contributed, and how many were estimated versus reported, is
  visible only in the cost report's tables.
- The usage advice line is produced only when at least one profile has an average. Its
  profile choice is the largest average; a tie goes to the row that comes first in
  sorted order, so the same data always yields the same line. The line names a generic
  change (a lighter profile for the role), not a specific profile, because the advice
  layer does not know which lighter profile the operator has defined.
- The advice line builds the cost report with an empty request, so it does not
  follow any filter the operator sets in the cost report. It describes the cost
  report's default view of the retained history. (The stats block's request was not
  checked for this draft and should be confirmed before filing.)
- No per-field applied or not-applied record appears on these surfaces as far as this
  slice goes. How a backend's refusal of a profile field would show up is covered by
  the sibling drafts, not by this measurement work.

## Expected behavior

An operator on any standard surface should be able to:

- **Read the start-up size per attempt**, with its source (estimated or reported), on
  the attempt and task detail, so a single attempt's start can be checked without
  opening the cost report.
- **Read the per-profile and per-role averages with their basis**, meaning how many
  attempts the average covers and how many of them were estimated versus reported. An
  average over five estimated attempts should not look the same as one over five
  reported attempts. The stats block today gives the average alone; the basis should be
  on the same surface, or the surface should say it is estimate-only.
- **See the advice line where usage is read**, with the profile it names, the average,
  and the attempt count behind it, and see that it is advice and changes nothing.
- **Get the same figures on every surface.** The MCP tool, the REST endpoint, the
  command-line output and any client surface must agree on an average because they share
  one aggregation. A client that cannot show a figure must say so rather than hide the
  surface that carries it, per the Mobile-Parity Rule.
- **Understand the no-data state.** A profile with attempts but no size should be shown
  as having no start-up data, with the count, not omitted and not shown as zero.

Capability parity is required; implementation parity is not. Each platform may show the
figures in its own idiom.

## Suggested next step

Fold this into the companion follow-up issue for worker start-up rather than filing a
separate issue, because the attempt-level figures only become meaningful once the
sessions' own start work lands on the daemon. Until a launch passes a profile, every
attempt is no-data or carries only the launch estimate, so an issue filed before that
point would describe numbers an operator cannot yet interpret.

Before filing, re-verify on a live daemon: that a launch on a profile produces an
`estimated` source on its attempt; that the stats block and the cost report agree on
one profile's average; and that the advice line appears only once some profile has an
average. Then, in the app: (1) on the attempt and task detail, show the start-up size
and its source; (2) beside the per-profile and per-role averages, show how many attempts
each average covers and how many were estimated versus reported; (3) where usage is
read, show the advice line with its profile, average and attempt count, labelled as
advice; (4) show a no-data profile with its attempt count rather than omitting it. If the
app team decides per-role and per-profile averages belong only on a daemon or terminal
surface, record that as an intentional exclusion with its reason in the issue. The
per-attempt start-up size should not be excluded, because it is the operator's only way
to see that the small start happened.

## Filing record

- Filed: — (pending; issue number and URL once opened. File after the sessions' start
  wiring lands, and re-verify the "Current behavior" section against the live daemon
  before filing.)
