# [sync] Daemon restart no longer loses in-flight planning — parity for the operator-facing clients (datawatch app)

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI (no `gh`, no
issue creation on a tracker) and creates nothing on any host.

**World this work changes** (found 2026-11-08): a deploy restart cost four
planners in flight. While a PRD was decomposing (`planning`), the daemon's
in-memory reader goroutine was the only thing standing between the PRD's plan
and the ground; when the daemon stopped — a deploy, an update, a crash — the
boot path reset the PRD to `draft` and killed the live planner session as an
orphan, and the operator had to requeue planning by hand. The operator
answered, quoted in
[`docs/plans/restart-matrix-findings.md`](../restart-matrix-findings.md):
"We should be able to do restarts when things are running. Why can't we?" and
"if we can get that working then you can do more restarts while things are
going on. Having to wait for a large queue is causing lots of delays and
getting things going forward". Decision, recorded 2026-11-08: **planning must
survive a restart.** This draft tracks the client side of that change.

## Affected area

The lifecycle boundary between a daemon restart and the sessions the daemon
watches in memory — specifically the planner session behind a PRD in the
`planning` state. A planner session lives in a tmux session on the host,
outside the daemon's process tree, so the *machine doing the planning keeps
running* when the daemon stops; what dies is the daemon's in-memory reader
that consumes the planner's output. The old boot behavior treated that as the
PRD's loss: PRD back to `draft`, planner session
ended — the old behavior is quoted in `docs/howto/autonomous-recovery.md` as
"ending the decompose session and resetting the PRD to `draft`" — and the
operator requeues by hand. The changed
behavior keeps the PRD in `planning` across the stop and, at boot, does one of
three things in order: re-attach a fresh reader to the still-live planner
session and its output file; ingest a complete plan from a planner session
that finished while the daemon was down; or, only when neither yields a usable
plan, restart planning **once** — a bounded single auto-replan, not a
re-plan storm — after which the plan proceeds to review (approve / request
revision) exactly as if planning had never been interrupted. The PRD status
vocabulary does not change: `planning` (BL221) and `needs_review` (BL191) are
long-standing `PRDStatus` values every client already renders.

What this work deliberately does *not* change, and clients must keep
tolerating as today: a daemon-run verification script that was running at
the stop does not survive the restart (an operator pick, still open, and the
matrix skips it — see the *Still to review (operator)* checklist), and a task verifying through a
live verifier session survives with its worker, retry and session counts
intact, but the in-flight verifier is not adopted: it runs once more,
and the latest strict matrix run also reports a duplicate session on that
row (found in the matrix, not fixed yet) — a row that the
[`docs/operations.md`](../../operations.md) *Restart safety* section and the
state rows in
[`docs/plans/restart-matrix-findings.md`](../restart-matrix-findings.md)
both record as surviving except that detail.

The operator guidance now lives in the *What happens to Automata when the
daemon restarts* section of
[`docs/howto/autonomous-recovery.md`](../../howto/autonomous-recovery.md),
the *Restart safety* section of
[`docs/operations.md`](../../operations.md) — including the *Planning not
restored after daemon restart (PRD was mid-decompose)* troubleshooting entry
and its old-build diagnostic ("Re-queue the plan with
`datawatch autonomous prd-plan <id>`") and the *Still to review (operator)*
block — plus the *Daemon restart while planning (decompose in flight)*
pitfall under *Common pitfalls* in
[`docs/howto/autonomous-planning.md`](../../howto/autonomous-planning.md),
the *Planning restart behavior* entry in
[`docs/datawatch-definitions.md`](../../datawatch-definitions.md), and the
*Daemon restart while a decompose is in flight* bullet in
[`docs/howto/daemon-operations.md`](../../howto/daemon-operations.md). The
*Still to review (operator)* block in the *Restart safety* section is the one
carried into these pointers at the time of writing:
the decision is settled, the implementation is in flight on the story that
recorded it (Automaton `af1e0001`), and the release smoke keeps the planning
row skipped until the matrix state implements the decision. Until that lands
and is live-verified on a real deploy-restart, the PWA side of this draft is
recorded as *decided and being implemented*, not *live-confirmed*.

## Current behavior (what this work delivers on the server and PWA)

The client-visible world this work produces, once the boot path lands:

1. A PRD that was `planning` at the daemon stop is still `planning` after the
   restart — it no longer drops to `draft`, and the planner session is no
   longer killed at boot. On a machine without tmux (or with the daemon in
   `subprocess` mode) there is no tmux session to carry the work, so the
   planning row does not survive — the described survival is tmux-based
   (the planner session lives on the multiplexer server outside the daemon's
   tree) and has no non-tmux counterpart.
2. The bounded single auto-replan at most means a client may see a *second*
   planner session for the same PRD. That second session is the expected
   "nothing usable to re-attach, re-planned once" outcome — it is not a
   duplicate-session anomaly and must not be surfaced as one.
3. The PRD arrives at `needs_review` exactly as if planning had completed
   uninterrupted, so the existing review controls (approve, request
   revision, plan-review verdict) are the only actions the operator needs.
   No new endpoints, no new fields, no new decision-log kinds: the PRD
   status enum and its transition rules are the only fact that moved.
4. The existing `GET /api/autonomous/restart-interruptions` surface (also
   `autonomous restart-interruptions` on the comm channel) still reports how
   many running tasks and live sessions the restart interrupted — but for a
   planner session, *interrupted* now means *re-attached at boot*, not *killed
   and lost*. The count is the same; the meaning of the counted session
   changed.
5. The old boot outcome for a mid-decompose PRD (session ended, PRD reset to
   `draft`) goes away; the new boot outcome is one of re-attached /
   ingested / re-planned-once / not-survivable (no tmux or subprocess mode).

The gap this leaves is entirely on the operator-facing mobile clients. As the
app stands, its understanding of what a daemon restart does to a `planning`
PRD encodes the old behavior: `draft`-after-restart was the machine-readable
signal that "planning was lost — re-plan by hand", and any client copy,
troubleshooting card, or auto-recovery affordance keyed on that transition is
now wrong for builds carrying the boot path. A client that treats two planner
sessions for one PRD as a duplicate-session bug will flag the bounded
auto-replan. A client that displays restart-interruption counts will show a
planner session it is no longer able to promise the operator was killed and
lost. The troubleshooting entry in
[`docs/operations.md`](../../operations.md) — *Planning not restored after
daemon restart (PRD was mid-decompose)* — keeps its old-build diagnostic
verbatim: "If the PRD was reset to `draft` instead: the build in use does not
yet carry the restart-safe planning boot path. Re-queue the plan with
`datawatch autonomous prd-plan <id>`". That stays valid as a diagnostic for
*old builds* (and as the operator's manual fallback for the no-tmux /
`subprocess` rows, which do not survive by design) and must not be presented
as the new-build outcome.

## Expected behavior

Once parity is met, a client on any platform can do what the PWA can: watch a
PRD through a daemon restart and keep showing it as `planning` (not `draft`)
the whole time, then show it at `needs_review` with the ordinary review
controls — approve, request revision, and the plan-review verdict — working
exactly as if planning had never been interrupted. The bounded single
auto-replan must read as one expected recovery step of the PRD, not as a
second planner session, a duplicate, or an error. A client displaying the
restart-interruption report must not tell the operator that an interrupted
planner session was lost; interrupted now means re-attached. Any client
copy, card, or flow that says "your plan was lost in the restart, re-plan by
hand" as the *expected* outcome must not appear on builds carrying the boot
path — it is reserved for the old-build diagnostic (and only when the PRD
actually did end up back at `draft`). In the absence of all of that, a client
must at minimum not misrender a PRD that stays `planning` across a daemon
restart as stuck, lost, failed, or an error state, and must not block the
operator's approve/revise actions once the plan lands at `needs_review`.

## Suggested next step

File one `fix(autonomous):`-prefixed issue in datawatch-app covering the
client side of this world — the `planning`-across-restart status render, the
"one bounded auto-replan" not being flagged as a duplicate, the
restart-interruption count's new meaning for planner sessions, and the
retirement of any "plan lost, re-plan by hand" expected-outcome copy —
referencing this draft as the source of truth for wording and shape, with
`docs/howto/autonomous-recovery.md` and the `docs/operations.md` restart-safety
sections as the operator-facing references. Where the app already renders PRD
status and review controls generically from the REST payload (which it should,
since no contract changed), the acceptance criteria are a pass-through test, on
both Android and iPhone: a PRD left in `planning` across a daemon restart must
stay `planning` on screen, then reach `needs_review` with a working
approve/request-revision pair; a PRD that re-planned once must not raise a
duplicate-session flag; and a seeded restart-interruption report showing a
planner session must not read as "lost". If any of those is structurally new
to a client, that client gets the dedicated handling described in Expected
behavior, with the same capability parity on both platforms and no
implementation parity required. Note the honest caveat carried by the source
docs: the boot path is decided and being implemented on Automaton `af1e0001`,
so the issue is filed *against the decided behavior* and can be verified only
once that story is live-confirmed on a real deploy-restart — the issue body
should say so.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included | No new endpoint and no new field: the PRD status transition rules on the existing PRD endpoints changed (`planning` survives the restart instead of resetting to `draft`; `needs_review` then follows), and `GET /api/autonomous/restart-interruptions` counts the same sessions with a changed meaning for planner sessions. Clients parsing PRD status must accept the new transition reality. |
| MCP | included | The autonomous MCP tools surface the same PRD status, decisions, and review actions — the changed world is data-flow, not contract; pass-through must not flag it. |
| CLI | included | `datawatch autonomous prd-plan <id>` (alias `prd-decompose`) stays the manual fallback for old builds and for the no-tmux / `subprocess` rows that do not survive; `datawatch autonomous restart-interruptions` reports the changed-meaning count. No CLI behavior this work changes, but the CLI is the recovery path the old-copy clients point at. |
| comm channel | included | `autonomous decompose <id>` and `autonomous restart-interruptions` carry the same status and count; any comm-surface copy keyed on "planning lost at restart" retires on builds carrying the boot path. |
| YAML/config | included | No new setting: the boot path is a behavior, not a knob, and `autonomous.resume_max_age_seconds` (which bounds the *resume* path) does not apply to the planning row. Recorded so the issue drafter does not invent a config surface. |
| PWA | included | The Autonomata view and PRD detail show the `planning`-across-restart → `needs_review` flow with the ordinary review controls; the operator-facing copy for a restarted daemon lives in `docs/howto/autonomous-recovery.md` (what happens to Automata when the daemon restarts) and `docs/operations.md` (Restart safety, including the troubleshooting entry and the *Still to review (operator)* checklist), both still flagged for live confirmation until the boot path is verified on a real deploy-restart. |
| Android | excluded | No app code lives in this repo. Capability parity is required and is tracked by this draft: `planning`-across-restart render, bounded auto-replan acceptance, restart-interruption count meaning, old-copy retirement — implementation parity is not. |
| iPhone/iOS | excluded | Same as Android: no code in this repo, and the same items are tracked here for the native client. |
