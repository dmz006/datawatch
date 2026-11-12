# [sync] Release approval: the daemon's merge path now honours the mode, and `ask` is a yes-only gate on the operator's decision (Phase 4 of the autonomy handover plan)

**Filed 2026-11-09 as dmz006/datawatch-app#419, one consolidated issue for all the release approval drafts** (shipped in v9.116.0).

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Story** (253e0001, "Part of finishing review and merge by the daemon (Phase 4
of the autonomy handover plan)"), operator 2026-11-07: the release approval
modes that the setting now carries (`auto`, `ask`, `manual` per size, resolved
project → global → default) are now **acted on** by the daemon's own merge run.
The gate keeps back the leaving steps — tag, push, deploy and the tag, deploy
and post-ship-hook steps — and only lets them run when the mode for the run's
release size says to.

## Affected area

What this work lands, in `internal/mergepipeline` and `internal/server`:

- **The pure decision.** `DecideRelease` (release_gate.go) maps a mode, a
  release size and the state of the pipeline's checks to one of three
  outcomes — ship, hold for a person, or hold for the operator's decision —
  with the ground rule first: a failed check never ships, in any mode, and
  never asks. A mode that is not one of the three resolves to `ask`, so an
  unknown mode is never a path to shipping, and a size that cannot be read
  resolves to `ask` rather than being shipped.
- **The ask, once and only once.** `RaiseReleaseDecision`
  (release_gate_ask.go) raises one yes/no decision question, `asked_by
  release-approval`, labelled `decision`, ending "Ship this release?",
  carrying the release plan in plain words: the project, the Automaton, the
  size, the version the release would get, the branches it merges, the checks
  and their pass/fail results, and the leaving steps in the order they would
  run. The options are `yes` ("yes (ship it)") and `no` ("no (do not ship)"),
  **"no" is the presented default**, and the question's stable identity is the
  hash of the asker, project, Automaton and size — a re-run for the same release
  finds the question already open rather than raising a second one. With no
  question channel wired it raises nothing and the run holds, which is the safe
  side.
- **The answer.** `ReleaseDecisionAnswer` (release_gate_answer.go) reads the
  question back through the question store and reduces it to the one fact the
  gate needs — did the operator say yes? An unanswered, withdrawn or missing
  question, and a "no", all hold. The answer that counts is the newest one that
  has not been superseded, so a changed mind replaces the earlier answer, and a
  held release is always distinguishable from an undecided one.
- **One verdict per run.** The gate runs at most once per run and is cached, so
  the run cannot answer the push differently from the tag: the first leaving
  step the run reaches decides for all of them (release_gate_run.go). The
  preparation — merging the branches, the version bump, the changelog, the
  commit — lands as usual in every mode; what is held back in `ask` and
  `manual` is only what leaves the machine.
- **The wiring.** `run.go` carries the runner's `ReleaseApproval` mode, the
  run's `ReleaseSize` (read where the release-version step learns the version,
  so a run with nothing to read is unknown-size and resolves to `ask`) and the
  question reader; `server/merge_run.go` resolves the mode for the project and
  size — `EffectiveReleaseApproval(project, size)` — and hands it to the run,
  and `supervisor_loop_wiring.go` runs the same gate on the daemon's own merge
  through the `ExecShipRunner` leaving steps.
- **The operator's ship line.** In `ask`, the first leaving step that runs
  records that the release was approved by the operator with the question id,
  so the ship is attributable to the answer that let it through. In `manual`,
  the leaving steps are held for a person and no question is raised; in `auto`,
  the run ships as before.

This closes, on the daemon side, the two gaps this story's companions tracked:
the "[sync] no merge path honours it yet" gap of
[2026-11-08-release-approval-parity.md](2026-11-08-release-approval-parity.md)
(the setting's value now decides the leaving steps) and the "[web] nothing
acts on them yet" gap of
[2026-11-08-release-approval-settings-web-parity.md](2026-11-08-release-approval-settings-web-parity.md)
(the Settings page's modes are the value the daemon's merge run reads). The
read-surface coverage of
[2026-11-08-release-approval-view-parity.md](2026-11-08-release-approval-view-parity.md)
stands as this draft's companion on the client side.

## Current behavior

On the daemon side the mode is now law for the leaving steps of the daemon's
merge run. A run whose checks pass ships under `auto`, holds under `manual`,
and under `ask` raises (or finds) its one decision question and ships only when
that question is answered yes and has not since been superseded; a run whose
checks fail holds for a person in every mode and never asks — a "yes" against
failed checks could only be honoured by shipping them, so the answer does not
reach the steps. A release whose size cannot be read holds under `ask` rather
than guessing, and the operator's changed mind on an open question is the
current word. The operator sees the mode in effect through the read surfaces
the companion drafts list, and when the gate holds a run the question's text is
the release plan in plain words, so the decision is made on the release itself,
not on an id. What `ask` means to the operator is exactly what the PWA and the
five locales already say: a yes ships it, a no — or no answer — holds it, and
the default is the hold.

## Expected behavior

Once parity is met:

- **The daemon.** A daemon merge run under `patch`/`minor`/`major` mode ships
  that size only where the mode says so — `auto` with the checks passing,
  `ask` on the operator's current yes, `manual` never without the operator —
  and a run that held for a question the operator has since answered yes
  proceeds, because the answer already exists rather than a second question
  being raised. This is the enforcement the [sync] draft asked for, and it
  applies to the daemon's own path, not just to the one that reads the setting.
- **The client.** The app's question surface presents the `release-approval`
  decision question the way the PWA does: the plan in the text (project,
  Automaton, size, version, branches, checks, leaving steps), its two options
  with their tradeoffs, and "no" as the presented default — a release that has
  not been answered is a release that does not ship. On a question the
  operator has already answered, only the current answer counts: the app does
  not need to re-ask, and a changed mind it surfaces must be the one the daemon
  counts. The settings and the in-effect modes the [sync] sibling draft
  already names are part of the same screen the release decision is made on.
- **The shared truth.** The modes, the defaults (patch `auto`, minor `auto`,
  major `ask`), the refusal of anything else, the per-project override and the
  resolved-in-effect read are one vocabulary on every surface — daemon, CLI,
  MCP, comm, PWA and app — because they are read from one place and the
  gate's own wording for a hold names the mode and, where it matters, the
  question id.

## Suggested next step

File this on the datawatch-app tracker as the third item of the release
approval set — after the setting and the status, this is the *decision*
item — and close it in the same decision as its two siblings: the app renders
the `release-approval` question with the plan, the two options and the default,
and its changed-mind presentation matches the supersede-on-newest rule the
daemon counts. Accept it against a daemon set to a size in `ask` mode where
the run can hold: the app shows the question with "no (do not ship)" presented
first, a "no" holds the release and a later "yes" on the same question — not a
second one — lets the run proceed to its first leaving step, which records the
operator's approval with the question id; and under `auto` the same app screen
sees the ship with no question at all. Nothing in this draft changes the REST
or MCP contract the client already renders against — the question travels the
existing `release-approval` question store channel the app's question surface
already reads, the settings and the field the companion drafts name — so the
client work is a render-and-settings pass on one screen, not an integration.
On the daemon side, the remaining Phase 4 work is the shadow pass the plan
names — the daemon's merge running beside the supervisor's manual merge with
every difference recorded as a defect — and the push-and-tag step beyond the
leaving steps this gate already holds; file those as their own drafts when
they land, the same way this one sits beside its companions under
`docs/plans/parity-issues/`.
