<!-- Filed 2026-11-09 as https://github.com/dmz006/datawatch-app/issues/406 (release v9.113.0). -->
# Release size rules for the daemon merge (Part of finishing review and merge by the daemon, Phase 4 of the autonomy handover plan)

## Affected area

The release size of an Automaton — the version step its merge adds (patch, minor, major or none) — in
`internal/autonomous`: the record fields added to the Automaton model (`release_size` and
`breaking_change` in internal/autonomous/models.go), the `milestone` flag on the Automation (arc) in
internal/autonomous/automation.go, the size vocabulary and normalization in
internal/autonomous/release_size.go, and the layered resolver `ResolveReleaseSize` in
internal/autonomous/release_size_resolve.go. On the settings side this is the release block of the merge
pipeline, internal/config/merge_pipeline.go: `autonomous.merge_pipeline.release_size_map` (overridable
per project under `autonomous.merge_pipeline.projects.<project>`), `major_trigger`, `release_cadence` and
`version_scheme`, with the layered `Effective*` readers. The tests live in
release_size_fields_test.go, automation_milestone_test.go, release_size_resolve_test.go and
merge_pipeline_release_test.go. This work deliberately touches **no** operator surface: no REST route, PWA
card, CLI command, MCP tool or messaging text presents these values, and the merge runner does not consume
the resolved size. It is the `603b0001` slice of the Phase 4 arc (the autonomy handover plan, operator
2026-11-07: "b finsih now we need to be using it to debug it and monitor its right"), filed with its
sister Automata (`253e0001` approval modes, `99be0001` merge lock and validator, and the rest of the arc)
for decomposition.

## What this work closes

Before it, the size of a release was convention: the supervisor decided a minor release by hand and left
the major to the operator by rule only (the plan's reading of the same evening: "Today there is no such
setting … the supervisor decided minor releases by hand and left a major release to the operator by rule
only"). Now the size is a pure, layered resolution — explicit `release_size` on the Automaton first
(including an explicit `none` and an explicit major), then a completed Automation marked `milestone` while
`major_trigger` is `milestone`, then the project kind map, then the built-in (feature minor, bug patch,
other kinds no step) — that never reads the diff, never infers a major from a kind, and gives the same
answer on every surface because it takes plain values and does no I/O. A reserved vocabulary is refused at
the setting's own path with the key named (`batch` and `manual` cadences, non-`semver` schemes, unknown
sizes and triggers), so a rejected value tells the operator exactly what to change, and unset settings read
the documented defaults. The docs set (the merge pipeline howto, the definitions entries, the operations
entry and `config-reference.yaml`) matches the implemented behavior.

## Current behavior and the gaps this leaves

First, nothing consumes the result. `ResolveReleaseSize` and the `Effective*` helpers have no non-test
caller: the merge runner's version-bump step does not read the resolved size today, so an explicit
`release_size`, a milestone arc, a kind-map entry and the `breaking_change` flag all resolve to an
answer that changes no ship's bump until the merge path is wired to it. The definitions and operations
entries say so plainly ("the merge runner's version-bump step consuming it is wired in the following
work"), so the behavior is documented honesty rather than a silent gap — but the resolved size is, at the
moment, a report the daemon can compute and a record it can store, not a decision it acts on.

Second, the approval side is not here (it is `253e0001` by design): there is no mode setting for
patch/minor/major yet, so the resolver's `RaisesMajorApproval` — true whenever `breaking_change` is set
whatever the size — is computed but unenforced. "Major stays with the operator" is therefore still the
plan's rule, not an `ask` mode on all six surfaces, and the promise that "a breaking change is never
merged quietly" is carried so far only by the flag and the docs.

Third, the milestone layer trusts a caller: `ReleaseSizeAutomation.Completed` is a plain boolean input,
and no code in the repository computes "every member of this Automation has finished" and feeds the
resolver. Layer 2 is therefore only as careful as whoever calls it, and the one rule in Phase 4 that the
operator's own words anchor ("at project level that will probably define a major change across all minor
updates … until the full major is ready for release as a working featureset") has no single definition of
"ready/complete" the daemon uses.

Fourth, no surface exists. `release_size`, `breaking_change` and `milestone` can currently be set and read
only in the records themselves and the settings resolved only from YAML and the internal Go API; the parity
table in the autonomy handover plan (one mode and threshold block per step on REST, MCP, CLI, comm, YAML,
PWA, and app issues) describes where these must end up, and none of it exists yet. Because the app is
unchanged in this release, no datawatch-app issue is due yet — but it becomes due the moment any of these
values or settings appears on a route, card or command that the mobile app renders.

Fifth, the reserved values stay reserved on purpose: `release_cadence` accepts only `per_merge` and
`version_scheme` only `semver`, matching the plan's "Version 1 covers … per-merge cadence and semantic
versioning; batch cadence and other schemes are the next step". That is a parity choice to keep, not a gap
to close early.

## Expected behavior

When the arc is done, an operator can set an explicit size on an Automaton, mark an arc a milestone, name
a breaking change, and set the four release keys of `autonomous.merge_pipeline` (per project), on the six
surfaces; the daemon resolves the size by the same layered rules this work ships, the merge runner's
version-bump step uses that resolution to decide the version step it makes, `breaking_change` and a
resolved major go through the major approval mode (default `ask`) instead of being merged, and the
`milestone` layer's "completed" means the daemon's own Automation-completed state, not a value a caller
asserts. `batch`/`manual` cadences and non-`semver` schemes arrive only with their implementations
removing the refusal, and every value and refusal that exists in code has a matching entry in the howto,
definitions, operations, and `config-reference.yaml`.

## Suggested next step

Track this as one daemon-side follow-up issue (datawatch, `internal/autonomous` and
`internal/config`), cross-referencing the Phase 4 arc in the autonomy handover plan and its sibling
Automata, rather than as an app issue — nothing in this release touches the app, so the datawatch-app
parity row ("each operator-visible change gets a datawatch-app issue at its merge") stays pending until
the first surface lands. The order the plan itself gives: first wire `ResolveReleaseSize` and the
`Effective*` helpers into the merge runner's version-bump step, with a test that the version step a ship
makes is the resolved size for each layer and that `none` makes no step, and replace the caller-supplied
milestone boolean with the daemon's own Automation-completed state; then land `253e0001`'s approval modes
so the resolved size and `RaisesMajorApproval` select `auto`/`ask`/`manual` (defaults patch auto, minor
auto, major ask) on all six surfaces with help entries, so `breaking_change` is enforced by a setting
instead of a rule; then open the first operator surface (REST and PWA minimum) for the record fields and
the four settings and file the datawatch-app issue in that same merge, recording its URL in this draft;
leave `batch`/`manual` and the other scheme refusements untouched until their implementations ship, and
keep the shadow-comparison discipline from Phase 4 (`d2f10001`) applying to every difference between the
daemon's resolved merge and the manual one it is compared against.

## Filing record

Not yet filed, and this draft was created without touching any tracker: no `gh` invocation, no issue
created on any host. File as a datawatch daemon follow-up issue (the resolver, the settings and the
unwired version-bump step all live in `internal/autonomous` and `internal/config`), cross-referencing
the autonomy handover plan and the Phase 4 arc `603b0001` (this work) plus `253e0001`; when its first
surface reaches the app, open the datawatch-app parity issue from that merge and record both URLs and
dates here. Drafted 2026-11-08; the code-level claims above (no non-test caller of `ResolveReleaseSize`,
the caller-supplied `Completed` boolean, the reserved-value refusals, and the docs wording) were checked
against the current tree at drafting time.
