---
title: "How to: Merge shadow"
description: "What the four merge shadow settings enable, their defaults and the values they accept, every way they are set, where the shadow state is read (REST, MCP, CLI, comm channel, PWA card) and what the daemon never does"
last_updated: "2026-11-17"
topics:
  - merge
  - shadow
  - pipeline
  - release
  - ship
  - automata
  - autonomous
  - settings
  - api
  - mcp
  - cli
  - comm
  - pwa
---

# How to: Merge shadow

The merge shadow settings control the **finish-time shadow merge plan**: when the
supervisor runs an Automaton's release, the daemon writes a *plan* describing what the
merge would do (release size, version, changelog heading, README line, registry row,
conflicts, gates, parity-issue flag) with `applied` stuck at `false`, and keeps the
plan in the shadow store. The four keys live in one settings block, are on by
default, and can be set from YAML, the API, MCP, the command line, the comm channel
or the web UI. The plan, and (once they are stored) the comparisons — and the
agreement streak read from them — are read — never acted on — from the REST
route, the MCP tool, the CLI and the comm channel; see "Where the shadow state
is read" below.

## What the merge shadow does — and what it never does

**Does:**

- Write one shadow merge plan per finished Automaton at the supervisor's release:
  the release size (`patch`, `minor`, `major` or `none`), the planned version
  (the version the changelog entry's heading names), the changelog heading and
  body drawn from the Automaton's own changelog entry task, then — when it has
  no usable entry — the changelog-draft artefact the branch recorded (its
  `Output`), then the new top entry the branch adds to `CHANGELOG.md` (nothing
  is invented when none of the three yields one), the README line, the registry
  row, and the parity-issue flag.
- Settle merge conflicts in a **throwaway trial worktree** outside the project
  tree, uncommitted (`--no-commit --no-ff`), then remove the scratch tree; the
  conflicts, each with its file class, resolution and resolver, are recorded in
  the plan. No branch, tag or ref is ever created or moved and nothing is pushed.
- Record each gate of the trial merge with its status — `pass`, `fail`,
  `timeout`, `skipped`, or `load_sensitive` (a test gate that failed first and
  passed when rerun alone) — and its duration, and each finding with a code and
  whether it blocks.
- Run the plan's checks before the trial merge: an Automaton task branch that
  holds commits the work branch does not blocks the plan with
  `unmerged_task_branch`, a recorded review verdict that names a commit other
  than the branch head blocks it with `review_stale`, and when either does the
  trial merge and every gate are then recorded as skipped
  (see "The plan's checks and gates" below).

**Never does:**

- Apply the release, bump the version, move main, create a tag, or push — the
  plan is a record, never a queue, and there is no second path back to the plan
  to apply it.
- Block, retry or fail the Automaton because a gate failed.
- Be a write surface for the operator: every read below is read-only, nothing
  is filed into the defect queue, and no gate, approval or automation consumes
  the comparison. Filing the recorded candidates into the defect queue is a
  separate job (see "What you still review" below).

## The plan's checks and gates

The plan does more than the trial merge. These checks run before it and inside
the gates — each is the replay of an event the phase's first tests meet again
(the autonomy handover plan, Phase 4):

**Unmerged task branch — `unmerged_task_branch` (blocks).** The first step of
the plan is the Automaton's own task branches: the ones named after the work
branch (`<branch>--tasks-…`). A task branch holding commits the work branch does
not blocks the plan with one finding per such branch, and the trial merge is not
run — every gate is recorded as `skipped` with the reason. That is the event
where a task branch is never merged back, replayed as a defect instead of being
discovered after ship.

**Stray root file — `stray_root_file` (blocks).** A check of an Automaton branch
as a merge candidate: one blocking finding per file the branch adds to the
repository root that the base ref does not track in its root tree, dotfiles
included. It catches the stray tracked file that would otherwise ride into the
merge at the repository root.

**Load-sensitive test gate — `load_sensitive` (reported as a pass, with the flake named).**
Only the `test` gate is rerun after a failure — a failed build or lint is never
rerun. The `test` gate that fails on its first run is run once more, alone. When
the rerun passes, the gate is reported as a *pass* — status `load_sensitive`,
which the plan's gate comparison counts as passing, not as a failure, and which
does not block the plan — with the flake named: its detail says it failed in
the first run and passed when rerun alone, and, from the run's `go test -json`,
names the tests that failed the first run and passed the rerun as
`flaky tests: TestA, TestB passed when rerun alone`; the first five lines of the
failing run are kept as the gate's finding (`finding: …`). When the rerun does
not pass, the first run's `fail` stands, with the rerun's status noted in the
detail (`rerun alone also …`). It is the daemon's answer to the test that fails
only under load: the gate names it, and the operator still decides what that
test is.

**Lint version pin — `lint_version_mismatch` and `lint_tool_unavailable` (block).** The `lint` gate runs the golangci-lint the trial merge's CI workflow pins — the `version` of a `golangci/golangci-lint-action` step in `.github/workflows/ci.yaml`, a `v`-prefix aside — not whatever happens to be on `PATH`. The daemon resolves that exact version in order: the binary in its own tools folder (`<data dir>/tools/golangci-lint/<version>/`), else the `PATH` binary when it already reports the pinned version, else it installs the pinned version into the tools folder (one install at a time, bounded to ten minutes) and uses that. The resolved binary's folder is put first on the gate's `PATH`, so `make lint` runs the pinned linter, and the gate's detail records the pin, the binary path, its version and where it came from (`tools folder`, `PATH` or `installed`). Because the daemon provides the pinned linter in its own tools folder, the host's own copy of golangci-lint — and what the host's tool updater keeps refreshing it to — no longer bears on the lint gate: its own merge pipeline no longer fails the lint gate on a host whose installed golangci-lint differs from CI. When the pinned version cannot be provided — no data directory, the install fails or times out, or the installed binary does not report the pinned version — the gate fails with one blocking `lint_tool_unavailable` finding naming the pin and the reason; a resolved binary that still differs from the pin fails with the blocking `lint_version_mismatch`. A CI workflow that pins nothing — no workflow, no such step, no `version` key — or one that cannot be read keeps the earlier behavior: the gate's status is unchanged and only the `PATH` golangci-lint's version (and, when the pin could not be read, the reason) is added to the detail. It is the answer to the lint that differs between local and CI, and to a daemon that could never pass its own lint gate. **Operator review:** the first gate run for a new pin downloads and builds golangci-lint, so it needs network and a Go toolchain on the daemon host and can take minutes; a `lint_tool_unavailable` finding means fixing that host, not the code.

**Plans-link gate — `check-plans-links`.** The gate runner runs the project's
make target of a given name — build, lint and test are the standard three in
the trial merge — and any other target can be run the same way, such as the
plans-link checker `check-plans-links`. A broken plan link fails that gate, and
the gate output reports only the count of broken targets; the checker never
names them. It is the answer to the broken plan link: a link that cannot resolve
keeps the merge from passing its gate.

**Review of an older commit — `review_stale` (blocks).** When a review verdict
on the work branch is recorded with the plan, the same step-0 run checks it
against the branch head: a verdict that names no commit, one that names a commit
the repository does not have, or one that names a commit other than the branch
head blocks the plan with a single `review_stale` finding — the review
examined some other commit, so its verdict says nothing about the work the
merge would land — and the trial merge and every gate are recorded as skipped.
A review of the current head produces no finding, and the check changes nothing
in the repository. It is the replay of the review that passed on a branch that
did not yet carry its fix.

**Failing tests named in the `test` gate.** When the `test` gate fails, its
detail names the failing tests — parsed from the run's `go test -json` output,
in the order they failed — and the first non-empty output line of the first
failing test (its first assertion), kept next to the original detail (e.g. `exit status 1`). A run that produced no JSON,
or none that failed, keeps its original detail unchanged. It is the answer to
the stale settings-count test: a test that fails because its expected count is
out of date fails the gate with its name and its assertion in the plan, not
lost in a wall of output.

**Hung gate suite — the timeout kills the whole process group.** Each gate's
process starts in its own process group, and every process the gate starts
inherits that group (on unix; elsewhere the timeout kills the gate's process
only, as before). When the gate's timeout ends, the whole group is killed, so
a hung PWA suite — one stuck on a browser or a server, and whatever it spawned
— ends together with the gate and nothing it started survives it; the gate is
recorded `timeout`. It is the replay of the PWA test suite that hangs: the gate
can only end by its timeout, and after it ends the suite is gone.

**Version bump that is not safe to run twice.** The planned version is the
version the changelog entry's heading names — a `## [version] - YYYY-MM-DD`
heading with a non-empty body, from the entry task, the branch's draft or the
branch's `CHANGELOG.md` — and the release size is the step between the base
version (the default branch's) and that planned one: a major when the major
number steps, a minor when the minor number steps, a patch otherwise; it is
`none` when either side is not a plain `X.Y.Z` version or the planned one is
not above the base — nothing is inferred in either direction. The trial tree's
own version is only ever compared against the planned one, never used to compute
it: the plan run twice on the same entry and base yields the same planned
version and the same size, and a trial tree that already carries the bump is
reported as already at planned (with the version files that differ from the
planned version — or hold no version at all — listed as stale). A bump applied
twice therefore cannot move the version twice.

## Where the shadow state is read

All five surfaces proxy one daemon endpoint and read — none of them writes:

| Surface | What it reads |
| --- | --- |
| **REST** | `GET /api/autonomous/merge/shadow` (read capability required; `405` with `Allow: GET` for any other method) → `projects[]`, each with `plans` (the stored shadow plans), `comparisons` (the stored shadow comparisons), `counts` (per release size `patch`, `minor`, `major`, `none`: matched and different comparison counts) and `streak` (`length` the most recent consecutive zero-major-difference comparisons, `total`, `threshold` and `met`; absent for a project with no stored comparisons). An empty `projects` list is the valid empty state, not an error. |
| **MCP** | `autonomous_merge_shadow_status` — read-only; the same answer, with an optional `project` argument to restrict it to one project. |
| **CLI** | `datawatch autonomous merge-shadow status` — matched and different counts per release size, plus `agreement streak N of T (met|not met, C compared)` per project, or `no merge shadow comparison exists yet` while nothing is stored; `datawatch autonomous merge-shadow compare <automaton-id>` — that Automaton's comparison: matched, different, not-compared, then each difference with its field, severity, planned value and landed value (or `No merge shadow comparison exists for Automaton <id>`). |
| **Comm channel** | `autonomous merge-shadow` — one summary line: `Merge shadow: patch 4 matched, 1 different; minor …; major …; none …; streak: a 2/3 not met, b 4/3 met`; `No merge comparisons yet` while nothing is stored. It takes no arguments. |
| **PWA** | The **Merge shadow** card on the **Events** tab (the Setup and policy sub-tab was removed): matched and different per release size, summed over all projects (plain figures — the card carries no links of its own). Rendered only when the shadow is enabled **and** at least one comparison has been stored — otherwise the card is absent from the page, which is the expected state, not a failure. |

**Why the comparison reads can come back empty.** The post-ship flow does not
persist a comparison record yet: the hook persists the **defect candidates**, and
the `merge-comparisons/` store exists in the code with nothing in the production
flow writing to it (see "What is still yours to do" in
[merge-pipeline.md](merge-pipeline.md#what-is-still-yours-to-do)). Until that
wiring lands, every comparison read above returns nothing: the `comparisons`
lists and `counts` stay empty, the CLI answers "no comparison … exists", the
comm channel answers `No merge comparisons yet`, and the PWA card stays hidden.
The stored **plans** are already listed per project on every surface. The defect
**candidates** have no operator-facing read at all yet — they are the JSON file
under the data directory (`<data directory>/defect-candidates/<automaton id>.json`).

## The four settings

All four keys live under `autonomous.merge_shadow` and are **on by default** —
the block behaves as if the defaults were written. An unset block and a block set
to its defaults are indistinguishable.

| Key | Type | Default | What it does |
| --- | --- | --- | --- |
| `autonomous.merge_shadow.enabled` | bool | `true` (when unset) | Master switch: when `true` the supervisor's release step runs the shadow plan; when `false` no plan is recorded at all. It is not a mode — there is no `on` state. |
| `autonomous.merge_shadow.gate_timeout` | duration | `20m` | The wait budget each gate of the throwaway trial merge may use before giving up — a per-gate budget, not a cap across all gates. When the budget ends, the gate's whole process group is killed (unix), so a hung suite leaves no children behind (see "Hung gate suite" in the checks section). A value that is not a positive Go duration is refused with the problem named on every write surface (nothing is written); a stored value that fails to parse falls back to the default at read time. |
| `autonomous.merge_shadow.plan_keep` | int | `200` | How many shadow merge plans are kept per project; anything older than the newest N is pruned. Values below `1` fall back to the default. |
| `autonomous.merge_shadow.agreement_streak` | int | `5` | The agreement streak threshold: the number of most-recent consecutive stored comparisons a project needs — with **no major difference** in any of them — before its streak reads `met`. It is the machine-readable form of the handover evidence bar, counted continuously. A value below `1` is refused on write; a stored value below `1` falls back to the default at read time. |

### How the streak is counted

- A comparison **breaks the streak** only when it holds at least one **major**
  difference (wrong or missing version, a planned heading, README line, registry
  row or parity issue that did not land, a planned gate missing from or different
  in the manual gate record — a planned `load_sensitive` against a landed `pass`
  (or the reverse) is not a difference, since both count as passing; a planned
  `load_sensitive` against a landed `fail` still is). A comparison that differs only
  in **minor** detail does not break it. The rule is pinned by
  `TestCompareLoadSensitiveCountsAsPass` in `internal/mergeshadow/compare_test.go`.
- `length` counts the most recent consecutive zero-major-difference comparisons
  of a project, newest first; `total` is the number of stored comparisons
  counted. `met` is `length >= threshold`, with `threshold` the configured value
  in effect (the default `5` when unset). A project with no stored comparisons
  has no streak at all — every read surface shows none for it.
- The streak is the bar the daemon's merge step comes on at: that step comes on
  only after the shadow has agreed with the merge that actually landed for the
  project's most recent `agreement_streak` merges — that is, only once the
  project's streak reads `met` — and until then the merge stays with the
  supervisor (see [Switching from shadow to on](#switching-from-shadow-to-on)).
  The switch itself is the supervisor's recorded decision on the Phase 4 release
  approval modes, taking the met streak as its evidence: no code path approves,
  switches or files anything on the strength of a streak alone. The streak
  resets silently when a major difference lands.

## Where the settings can be set

The settings are the same no matter which path writes them — one settings
block, one place it is read back from:

| Path | How |
| --- | --- |
| **YAML** | `config.yaml` → `autonomous.merge_shadow.enabled`, `autonomous.merge_shadow.gate_timeout`, `autonomous.merge_shadow.plan_keep`, `autonomous.merge_shadow.agreement_streak` |
| **REST** | `PUT /api/config` with any of the four keys (the `GET /api/config` read returns the merged default for each) |
| **MCP** | The generic `config_set` tool, with the key `autonomous.merge_shadow.<key>` and its value. The shadow has no dedicated MCP setter; `get_config` reads it back. |
| **CLI** | `datawatch config set autonomous.merge_shadow.enabled true|false` and friends |
| **Comm channel** | `configure autonomous.merge_shadow.enabled=false`, `configure autonomous.merge_shadow.gate_timeout=1m`, `configure autonomous.merge_shadow.plan_keep=50` |
| **Web UI** | Settings → Automation → **Merge shadow** group: one toggle (`enabled`) and two fields (`gate_timeout`, `plan_keep`). The state read above lives on the **Merge shadow** card on the **Events** tab |

## Reading the counts on each surface

The counts are tallied per release size, in the fixed order `patch`, `minor`,
`major`, `none`. One comparison is counted once: it is **different** when it
holds at least one difference, and **matched** otherwise. The tally is the
number of comparisons, not the number of fields.

- **REST** — `counts` on each project: `{"patch": {"matched": 4, "different": 1}, …}`.
  Nothing is summed; add the projects yourself.
- **MCP** — the same per-project `counts`; pass `project` to read one project.
- **CLI `status`** — one line per size, summed over all projects. Every size is
  printed, so a zero is visible.
- **CLI `compare <automaton-id>`** — one Automaton only. It prints its `matched`,
  `different` and `not_compared` field counts, then each difference as
  `<field> [<severity>]: planned “…”, landed “…”`. A missing landed value shows
  as `(missing)`.
- **Comm** — one line, `Merge shadow: patch 4 matched, 1 different; …; streak: a 2/3 not met, b 4/3 met` (the `streak:>` part only when at least one project has a stored comparison).
- **PWA card** — the same per-size figures, summed over every project, shown only
  when the shadow is enabled and at least one comparison is stored. The card
  carries no agreement streak.

The `streak` on the REST, MCP and CLI surfaces is per project and uses the same
rule on every one (same function, same threshold, same zero-major-difference
test): `met` is the streak length reaching `autonomous.merge_shadow.agreement_streak`.

Read the empty states as facts, not faults. `projects: []`, `no merge shadow
comparison exists yet`, `No merge comparisons yet`, and a missing card all mean
the same thing today: no comparison is stored.

## From a difference to a defect candidate

Each difference the grader finds becomes one **defect candidate**. The steps:

1. **Grade.** Each field is compared with the landed value read from the default
   branch's ref. A difference is **major** when a version is wrong or missing, a
   planned changelog heading, README line or registry row landed nowhere, a
   required parity issue did not land, or a planned gate is absent from or
   different in the manual gate record. It is **minor** when a heading, README
   line or registry row drifted in wording, or a parity issue landed although
   the plan did not require one.
2. **Record.** The candidate carries its Automaton, field, planned value, landed
   value, severity, release size, status `candidate` and creation time. Its key is
   derived from that content, so the same difference is stored once.
3. **Stop there.** The candidate is written to
   `<data directory>/defect-candidates/<automaton id>.json` and nothing else. It
   is not filed into the defect queue, it does not alert, and it does not hold or
   block anything. Filing is the defect intake's job, and no read surface shows
   candidates yet.

Two lines cannot yet produce a difference from the shipped flow, so a silent
match there is not evidence. The parity-issue line compares the plan's flag with
itself, so a required parity issue that never landed is not caught. The gate line
has no manual gate record in that flow, so gates are counted as *not compared*,
never as failures. Count these as known blind spots when you grade a merge by
hand.

## Switching from shadow to on

The shadow itself has no `on` state: `autonomous.merge_shadow.enabled` is only a
switch for writing the plan. The shadow-to-on decision belongs to the **release
approval modes** of Phase 4 of the
[autonomy handover plan](../plans/2026-11-03-autonomy-handover-plan.md). Those
modes (`auto`, `ask`, `manual`, one per release size) are designed and filed as a
draft; **no approval-mode setting exists yet**. Until they land, the daemon's merge
runs in shadow beside the supervisor's manual merge, and nothing is switched.

The handover plan sets the switch's terms, recorded here so the evidence is read
the same way every time:

- **Evidence bar.** Ten merges where the daemon's plan equals the manual merge or
  is better, at least two with conflicts, at least two with PWA changes, and no
  blocking comparison defect. The per-project agreement streak is its running,
  machine-readable measure: the daemon's merge step comes on only once that
  streak reads `met` — the shadow has agreed for the project's most recent
  `agreement_streak` merges.
- **Who decides.** The supervisor validates the checks and reports the
  comparison. Patch and minor modes are the supervisor's to move toward `auto` as
  the daemon improves. The major mode stays `ask` unless the operator changes it.
- **Record.** Each mode change is kept as a decision with its evidence.
- **Falling back.** After a switch, a size drops back to `ask` on a failed
  validator, a red release CI within its window, or a merge that needed a manual
  fix. The daemon raises an alert and files a defect.

Before you use that bar, know its limit: the comparison records are not stored
yet, so the evidence has to come from the stored plans and the defect candidates
in the data directory. Check the plan against the manual merge by hand until the
comparison reads show data.

## Do not confuse

- **Merge shadow (`autonomous.merge_shadow.*`)** — the shadow merge plan: what,
  recorded, `applied` stuck at `false`. This page.
- **Merge pending (`autonomous.automation.merge_pending_*`)** — the live
  *alert* that a finished Automaton's merge has not landed on main. It is a
  different settings block with a different job
  (see [automation-tab.md](automation-tab.md)).
- **Merge pipeline (`autonomous.merge_pipeline.*`)** — the *real* merge: the
  steps that move the release. The shadow plan reads the pipeline's steps to
  know what would happen (see [merge-pipeline.md](merge-pipeline.md#the-shadow-merge-plan)).

## What you still review

The merge shadow never applies, files or blocks — so the review is on you. Read
the stored plan for any finished Automaton (REST, MCP, the CLI `compare` or the
comm channel; the PWA card shows the comparison, not the plan), then review:

- Every difference the shadow comparison grades — that is the contract with the
  manual merge; the comparison is evidence, not a gate.
- Every defect candidate in the data directory's
  `defect-candidates/<automaton id>.json` — the candidates have no read surface
  today, and filing them into the defect queue is a separate job.
- The plan's findings that name a test to watch or a merge to fix: the
  `load_sensitive` gate (a test that failed under load and passed when rerun
  alone is still a test to watch), the failing tests named in a failed `test`
  gate's detail (a stale expected count is a test to fix before the next merge),
  and the blocking `unmerged_task_branch`, `stray_root_file`,
  `lint_version_mismatch`, `lint_tool_unavailable` and `review_stale` findings (a `review_stale` review
  is a verdict on an older commit — the current head is not reviewed yet, so
  review it before the merge lands) (see
  [The plan's checks and gates](#the-plans-checks-and-gates)).
- The parity-issue and gate lines in the plan, per
  [merge-pipeline.md](merge-pipeline.md#what-is-still-yours-to-do).
- The fact that comparison records are not persisted yet — the comparison
  surfaces above stay empty until that lands; the plan is the one read that
  already works.
- Whether the Phase 4 evidence bar is met before you step out of the loop entirely —
  the shadow is how you collect that evidence, and the per-project streak
  (threshold `agreement_streak`) is whether it has been met for that project
  ([2026-11-03-autonomy-handover-plan.md](../plans/2026-11-03-autonomy-handover-plan.md),
  Phase 4).

## Related

- [merge-pipeline.md](merge-pipeline.md) — the walkthrough; the shadow merge plan in
  `merge` and the settings block it reads.
- [api/autonomous.md](../api/autonomous.md) — the autonomy endpoints the daemon
  exposes (the shadow state is `GET /api/autonomous/merge/shadow` beside the
  merge reads).
- [the merge-shadow settings block](../datawatch-definitions.md#merge-shadow) — the
  settings block as a reference.
- [the autonomy-handover plan](../plans/2026-11-03-autonomy-handover-plan.md) — the
  shadow → on → off handover that these settings serve (Phase 4).
