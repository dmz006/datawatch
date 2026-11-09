# [parity] worker session profiles: the docs set says all six daemon surfaces agree, and nothing on any surface — daemon or app — enforces that (datawatch-app follow-up)

**Filed 2026-11-09 as dmz006/datawatch-app#418** (shipped in v9.115.0).

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened on the
`dmz006/datawatch-app` repository when the operator files it. Filing the issue
does not change a line of this repo. Draft written 2026-11-09 against the work
for "Make every worker session start small, with context and tools chosen for
its job, configurable for every datawatch user." (PRD `24230001`), specifically
the slice this story covers: the combined docs set (`docs/howto/worker-session-profiles.md`,
the Worker session profile / starting context / start-up token size entries of
`docs/datawatch-definitions.md`, and the index rows in
`docs/README.md` and `docs/howto/README.md`) and the launch-pipeline
integration test
(`internal/autonomous/worker_profile_integration_test.go`), both landed. The
companion drafts `2026-11-08-worker-small-start-parity.md` (the two mechanisms a
launch applies — the pinned MCP set and the deterministic starting context) and
`2026-11-09-worker-profile-settings-surface-parity.md` (the six daemon
config surfaces and what the app lacks for each) cover the other two sides of
the same question; this draft covers the docs and the test that lock the
behavior they all describe, and the parity it does not yet have.

Parent release: the datawatch release that ships worker session profiles — the
feature currently under `[Unreleased]` in `CHANGELOG.md`.

## Affected area

The operator-facing description of the feature, and the one test that makes the
description provable. Three places carry what the feature is and does for a
reader who has never set a profile: the standalone how-to
(`docs/howto/worker-session-profiles.md`), the three glossary entries it
defines in `docs/datawatch-definitions.md` (**worker session profile**,
**starting context**, **start-up token size**), and the two hand-maintained
index rows in `docs/README.md` and `docs/howto/README.md` that let a person or
a model find either. On top of that sits the integration test:
`TestWorkerProfileIntegration_*` in
`internal/autonomous/worker_profile_integration_test.go` wires resolution, the
per-session MCP config writer, the starting-context builder, the attempt record
and the per-field applied/not-applied recording together against fakes only —
no process started, no file outside a test directory touched — and is the test
a reader's trust in the how-to's "implemented and tested" claims ultimately
rests on.

From the operator's point of view the affected areas are: the single document
every daemon surface's help text promises (the web settings help lines, the
comm channel's `configure help`/one-line help, the CLI refusal messages all
point at `docs/howto/worker-session-profiles.md`), the glossary vocabulary the
how-to and the settings group's labels share ("Worker session profiles",
"Daemon-wide worker profile", the four built-in names, `minimal` / `digest` /
`full`), and the one executable statement that the surface table in the how-to
is true — that every layer of the resolution order (task → Automaton → project
→ daemon → task-kind default → role default) beats the less specific ones, that
a launch on a profile pins exactly the named MCP servers, that the same role and
profile start with byte-identical context, and that a field a backend cannot
honour is recorded as not applied rather than dropped.

## What this work closes

Before this work, the how-to, the glossary and the two index rows did not yet
agree with each other about where the feature stood: some places described the
start as having happened, others named parts of it that had not landed, and a
reader of the definitions got the feature's vocabulary without a place to
verify any of it. Today all four docs files describe the same state — the
mechanisms and the six-surface settings round-trip are **landed and tested**,
and the launches calling the rule are named precisely as the part still
**waiting on the sessions' own start work** (the index rows say "will start
small (with the sessions' own start work)", the how-to's "Where this stands"
section splits the two lists, and the glossary entry says worker, verifier and
judge sessions start with the full harness today). The vocabulary the PWA
settings group already uses, the MCP tool names, the CLI key spellings and the
comm channel keys are all in one table ("Setting and reading it") with the same
refusal wording every surface uses. And the integration test makes the
how-to's central behavioral claims — override order, the pinned MCP file with
its named refusals, the deterministic context's section order, the per-field
record in stable `mcp, tools, skills, context, model, effort` order — executable
statements a daemon change either passes or fails on, not prose.

None of that is the app's surface. It is the daemon's operator guide and the
daemon's own test — and the parity this slice leaves is in exactly the gap
between "the guide names six surfaces and a set of app-visible consequences"
and "every surface named actually has one, and everything that points at the
guide can reach it."

## Current behavior

What the docs set and the test leave open, on the surfaces they point at:

- **The how-to's surface table names consequences the app cannot show.** The
  "The measured starting context, where it shows" section says the profile an
  attempt ran under, its starting size, and the per-field record are readable
  on the attempt record — "the Automaton detail, the REST read, the MCP read"
  — and in the cost report, stats and usage line. On the PWA that is true; on
  the Android and iPhone clients none of those surfaces has the
  `worker_startup` block, the attempt's profile name or the applied record,
  and there is no settings group for the three keys the table's first rows
  describe. A mobile operator who reads the help link target (if they can
  reach it) learns what the feature does and then has no surface in their hand
  to see any of it. The companion draft
  `2026-11-09-worker-profile-settings-surface-parity.md` is the up-to-date
  list of exactly those missing app surfaces; this draft's claim is narrower —
  that the docs, which are the thing the app's future surfaces should copy
  labels and vocabulary from, currently point at a world with one surface per
  daemon channel and zero of them on the phone.
- **The help links into the how-to are single-surface today.** Every daemon
  surface that points at `docs/howto/worker-session-profiles.md` (the web
  group's help lines, the comm help text, the CLI refusal, the config reference
  and implementation cross-references) reaches it through the daemon's own docs
  serving. There is no app-side surface that does: the PWA help link works, but
  neither mobile client has an equivalent "read the operator guide for the
  setting in front of you" path, so the vocabulary a mobile operator is shown
  in the (still missing) settings group will have no canonical source they can
  open from the app.
- **Two of the docs' index rows are hand-maintained prose.** The
  `docs/README.md` and `docs/howto/README.md` rows describing
  `worker-session-profiles.md` were edited by hand to match the how-to's state
  ("will start small (with the sessions' own start work)"); nothing enforces
  the match after the fact. If the sessions' start work flips the how-to from
  "will start" to "starts", the rows stay "will" until a human notices — and
  those rows are the first thing a docs-search (person or model) hits, so the
  drift would land exactly where the operator looks first. The same is true
  inward: a rename or state change inside the how-to is not reflected in the
  glossary's three entries or in the rows, and there is no test that
  cross-checks the four.
- **The integration test pins the daemon's pipeline, not any surface's
  wording or shape of it.** The override-order cases, the named-server refusals
  and the deterministic-bytes assertions hold the *behavior*; the per-surface
  tests (REST pre-validation, MCP tool vocabulary, CLI key mapping, comm
  configure keys) hold the *channel* behaviors independently. Nothing guards
  that the channel behaviors *agree with each other and with the docs*: a
  future change that lets one surface accept a name another refuses, or that
  renames a key in one spelling, passes every test and only the how-to's table
  exposes it. The docs are the only artifact where all six surfaces sit in one
  place, and they are the only place with no test.
- **The "where this stands" split is load-bearing prose.** The how-to, the
  glossary entry and the two rows all repeat, in slightly different words,
  which claims are in-force and which land with the start work. That
  repetition is what keeps the PWA settings group (already shipped) from
  over-promising to a web operator — it shows the settings as set, and the
  docs say plainly that the launches carrying a resolved profile still land.
  The risk it carries is a one-way drift: when the start work does land, five
  separate prose locations in four files must each be flipped (plus the
  CHANGELOG entry and the two companion drafts' filing-record notes), and
  flipping four of the five leaves a reader on one of the app or daemon
  surfaces reading a state that is no longer the daemon's.

## Expected behavior

A parity state in which the docs the daemon surfaces promise are reachable,
match the behavior, and the match is enforced in the one direction that is
enforceable:

- **The guide is reachable from every surface that points at it, including the
  ones that are missing.** When the app's worker-profile settings group lands
  (per `2026-11-09-worker-profile-settings-surface-parity.md`), its help link
  should carry the same target the PWA group's help line already carries — the
  worker session profiles how-to — and the app's labels should be the
  glossary's vocabulary (built-in names, the six field names, "starting
  context", "start-up token size") in at least the five languages the PWA
  bundle already carries, so the doc, the web settings and the phone settings
  describe the same object with the same words.
- **The app's attempt-level read-out matches the docs' promise.** The how-to
  says the profile run under, the starting size and the per-field record are
  on the attempt record and in the cost surface; the Android and iPhone
  attempts detail and cost screens should carry that reading (profile name,
  size with its `estimated`/`reported` source, applied/not-applied per field)
  the same way the PWA's do. An attempt run on `full` should be marked as such
  on the phone, not hidden in the average, which is the sentence the how-to
  ends its measurement section with.
- **The four doc locations agree, and the agreement is checked.** The
  how-to's "Where this stands" lists, the glossary's three entries, and the
  two index rows should each state the same in-force / still-waiting split —
  today they do, by hand — and a docs-level check (the docs set's own verify
  step is the natural home for it) should fail when the how-to's state words
  (`will start small` once the start work lands, `starts small` today's
  complement) and the rows' and entries' state words stop agreeing, rather
  than letting the first row a reader hits describe yesterday's daemon.
- **The surfaces' agreement with each other is at least as guarded as the
  behavior is.** The integration test already proves, with fakes only, that
  resolution order, the pinned MCP file, the deterministic context and the
  applied record hold at the daemon's core; the cross-surface promise the
  how-to's table makes — that every channel refuses the same bad value and
  reads back the same key — should have its own guard in the test suite (a
  shared fixture asserting, for one good and one refused profile name, that
  the config file, REST, MCP, CLI and comm channels all end in the same
  state) so the table can never drift from any single one of its six rows
  while every per-surface test passes.

## Suggested next step

This is a client-side issue on `dmz006/datawatch-app` with a small
daemon-side note attached, and it should be filed **alongside** the two
companion drafts — the app team files one coherent "worker session profiles
parity" issue, this draft's sections folded in, not a third ticket. File
**after** the sessions' own start work lands, for the same reason the
companion draft says: until then the how-to's state split is honest and its
"will start small" wording is the expected reading, and the issue's expected
behavior (profile per attempt, size per profile, applied record per field) is
not yet observable anywhere to copy from. At minimum the app should:

1. **Carry the docs' help-link promise.** Wherever the worker-profile setting
   group or the attempt/profile read-out lands, a help link with the same
   target the PWA group's help lines already use, and strings taken from the
   glossary's three entries rather than newly invented words, in the five
   languages the PWA bundle carries (en, de, es, fr, ja), through the
   Compose pipeline per the Localization Rule.
2. **Read back the attempt-level facts the how-to names.** Profile name per
   attempt, starting size with its source, the per-field applied/not-applied
   record in the stable field order, and the `worker_startup` averages —
   sourced from the same stats snapshot and attempt records the PWA already
   reads, so phone and web show the same numbers from the same words.
3. **Surface the daemon's named refusals unchanged.** An unknown profile name
   or a user profile named like a built-in should reach the app with the
   daemon's own message (the valid names included), the way the PWA already
   shows a refused setting — the how-to's table promises the refusal behavior
   is shared, so the refusal wording should be too.

The daemon-side note, for this repo's next docs set after the start work
lands: flip the five state statements (how-to "Where this stands" and
"Shape of the start", glossary entry, both index rows), and add a docs
consistency check that fails on a mismatch between the how-to's in-force /
still-waiting split and the rows' and entries' state wording — the docs set
that lands the start wiring should carry that, or the drift described above is
guaranteed to happen on exactly those five locations. And if the app team
decides worker-profile display is a terminal-only advanced surface, record
that as an intentional exclusion with its reason in the issue — but the
attempt-level showing (profile run, context size, what applied) is the part
that should not be excluded, because the how-to's final section is a promise
that the operator can see the small start happened, and today no app surface
can keep it.

## Filing record

- Filed: — (pending; issue number / URL once opened; file after the
  sessions' start wiring lands, at which point re-verify the "Current
  behavior" section against the live daemon — specifically whether the
  per-attempt profile and `worker_startup` reads are non-empty — and update
  the expected-behavior list to match the labels the shipped start work chose
  if they differ from the ones already on the PWA. This draft names the docs
  locations and the test as of 2026-11-09; the two companion drafts
  (`2026-11-08-worker-small-start-parity.md`,
  `2026-11-09-worker-profile-settings-surface-parity.md`) carry the mechanism
  and surface detail this one only cross-references.)
