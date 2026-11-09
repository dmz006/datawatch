# [sync] Daemon restart now acts on `planning_restart_policy` for a planning Automaton — parity draft for the datawatch-app native clients (Android, iPhone)

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI (no `gh`, no
issue creation on a tracker) and creates nothing on any host.

**World this work changes** (found 2026-11-08): a deploy restart killed four
planners in flight. While an Automaton was planning (its decompose running),
the daemon's in-memory reader was the only thing standing between the plan and
the ground; when the daemon stopped — a deploy, an update, a crash — the boot
path ended the planner session and reset the Automaton to `draft`, and the
operator had to requeue planning by hand. The operator asked, quoted in
[`docs/plans/restart-matrix-findings.md`](../restart-matrix-findings.md): "We
should be able to do restarts when things are running. Why can't we?" The
decision recorded that day: **planning must survive a restart.** The sibling
drafts track the two halves of the client side already —
[`2026-11-08-daemon-restart-planning-survival-parity.md`](2026-11-08-daemon-restart-planning-survival-parity.md)
for the boot-path behavior and
[`2026-11-09-planning-restart-policy-parity.md`](2026-11-09-planning-restart-policy-parity.md)
for the `autonomous.planning_restart_policy` setting and its write surfaces.
This story (`8f990009` on Automaton `af1e0001`) is the half that *implemented*
the boot path the 2026-11-08 draft was filed *against the decided behavior*.
This draft is its successor: both sibling drafts' open caveats change status
now, and three new client-visible facts that the siblings never listed — the
new `boot_resume` outcome values, the new `planning_restarted_once` PRD
field, and the once-only-restart rule — become part of the expected surface.

## Affected area

The outcome a daemon restart produces for an Automaton still in `planning`,
and the two read surfaces that carry that outcome to an operator: the PRD
object, and the per-PRD boot-resume report on the loop status. Concretely, a
story file — under
`internal/autonomous/` (`models.go`, `manager.go`, `api.go`,
`decompose.go`, `planning_resume.go`, `planning_reattach.go`, `api.go` tests)
and `cmd/datawatch/` (`main.go`, `session_judgment.go`,
`planning_reattach.go` and their tests) — makes the boot path read
`autonomous.planning_restart_policy` and act on it:

1. The PRD records its planner at spawn — `planner_session_id` and
   `planner_output_file` — so a restart has something to find.
2. `DecidePlanningResume` (pure, in
   `internal/autonomous/planning_resume.go`) maps `(policy, planner record,
   session alive, output state, already-restarted-once)` to exactly one of
   four actions: re-attach a reader, ingest a finished plan, restart planning,
   reset to draft.
3. Boot runs that decision per planning PRD: under `reattach` (the default;
   unset and unrecognised read as it) it re-attaches a fresh reader to the
   still-live planner session (tmux) and its output file; if the session
   ended while the daemon was down and left a complete, parsable plan, it
   ingests that plan as a normal decompose result — the Automaton reaches
   `needs_review` with the ordinary review controls, and no new planner ran;
   only when neither yields a usable plan does it restart planning **once**.
   `restart` restarts planning once at boot; `draft` restores the old
   behavior.
4. The one automatic restart is a hard cap: it sets
   `PlanningRestartedOnce` on the PRD, and that field
   is **never cleared**, so a second restart with nothing usable left falls
   back to draft instead of re-planning again.
5. Live planner sessions no reader owns are ended, and the reason is in the
   recorded decision text.

Every outcome lands in two places an operator can already see: the
Automaton's decisions and the **boot-resume report** — the `boot_resume`
array on `GET /api/autonomous/status`, the same data `datawatch autonomous
status` prints, and the `boot_resume` array on the loop-state JSON on the
comm channel. The status vocabulary itself does not change: `planning` and
`needs_review` are long-standing `PRDStatus` values, and no new endpoint,
tool, config key, or decision kind was introduced. What this work does not
do: a `planning` PRD only survives on a machine where the planner session
lives outside the daemon's process tree (tmux); on a machine without tmux, or
with the daemon in `subprocess` mode, there is no session to re-attach and no
file to ingest, so the row does not survive — the boot path there does what
it always did.

Operator guidance for the new world already lives in
[`docs/howto/restart-with-work-running.md`](../../howto/restart-with-work-running.md)
(policy values, where to set them on every surface, current status), the
*Restart safety* section of
[`docs/operations.md`](../../operations.md) (including the *Planning not
restored after daemon restart* troubleshooting entry — whose old-build
diagnostic now reads "the effective policy is `draft` (the intended outcome
under that value), **or** the build in use does not yet carry the
restart-safe planning boot path") and the
*Planning restart behavior* entry in
[`docs/datawatch-definitions.md`](../../datawatch-definitions.md). The
honest caveat is in all three: the boot path is in this tree, but the
`planning` row of the restart matrix and its release-smoke run are still
open, and **no live deploy-restart has confirmed it yet**.

## Current behavior (what this work delivers, and the gap it leaves or closes)

What the client world looks like once a build carrying this story is in use:

1. **The `draft`-after-restart signal is gone as an expected outcome.** A
   PRD that was `planning` at the stop stays `planning` (or reaches
   `needs_review`) on a build with the boot path, unless the effective policy
   is `draft` — in which case the reset to `draft` is the *intended* outcome
   of that setting, not a loss. Any client copy, card, or auto-recovery
   affordance that keys "the PRD dropped to `draft` after a restart" as the
   expected loss-signal is wrong for new builds, and must read the policy
   before it explains the outcome.
2. **One extra planner session for one PRD is a normal recovery step, not a
   duplicate.** Under the `restart` policy, or under `reattach` when nothing
   usable survived, the bounded single auto-replan legitimately starts a
   second planner session for the same PRD. A client that flags two planner
   sessions per PRD as a duplicate-session anomaly will flag the expected
   recovery — the `planning_restarted` entry in the boot-resume report and
   the `planning_restarted_once` PRD field are the machine-readable markers
   that this is the one bounded restart, and a further restart deliberately
   resets to draft rather than re-planning again.
3. **Three new outcome values appear in `boot_resume` entries.** The report
   previously carried `resumed`, `parked` and `resumed_by_operator`; this
   story adds **`planning_ingested`**, **`planning_restarted`** and
   **`planning_reset_to_draft`** (the `attach_reader` outcome re-attaches a
   live session and records no report entry at all — so an operator who sees
   nothing new in the report but sees the planner run to completion can be
   *told* that a live planner was re-attached, and the client should not
   misread the missing entry as a missing decision). Each new entry's
   `reason` is plain prose that names the policy, the planner output file
   (for `planning_ingested`), the once-only rule (for
   `planning_restarted`), and any live planner sessions that were ended.
4. **New, read-only PRD facts.** `planner_session_id`,
   `planner_output_file` (both `omitempty`, present only while the PRD has a
   recorded planner) and `planning_restarted_once` (`omitempty`, set when
   the one automatic restart was used and never cleared). All are
   additive: a client that ignores unknown PRD fields keeps working, and the
   PRD status enum and every existing field are untouched.

What this story *closes* on the client side is the status question that both
sibling drafts flagged as open. The 2026-11-08 sibling was filed *against
the decided behavior* — "the implementation is in flight on the story that
recorded it" — and its acceptance tests (PRD stays `planning` across a
restart, one auto-replan not flagged as a duplicate, a planner session in the
restart-interruption report not read as "lost") are now testable against a
real build: this story is a merged implementation of the behavior those tests
assert against. The 2026-11-09 policy sibling carries three caveats that are
now stale — the PWA help text's "until the restart path reads the setting,
every value acts like `draft`", its own "the restart path that acts on this
setting is decided and in flight" note, and its expected-behavior item 4's
"under the `draft` policy (or on a build without the boot path)" framing —
all of which are superseded by "the boot path reads the setting at restart
and runs every value", as `docs/operations.md` (line *"Planning restart
values"*) and `docs/datawatch-definitions.md` (*Planning restart behavior*)
now state. An app still carrying that help text as its truth is making a
promise about the restart outcome that the operator may have deliberately
changed, and must not.

The gap this work still *leaves*, exactly as before and now with a sharper
edge:

- **No live deploy-restart has confirmed any of it.** The *Still to review
  (operator)* block in `docs/operations.md` is unchanged by this story:
  confirm on a build in use that a restarted daemon does not reset a
  mid-decompose PRD to `draft`, that a complete plan from an ended planner
  session is ingested at boot, and that a mid-decompose PRD whose planner is
  gone with no plan still gets exactly one fresh planning run; and the
  `planning` row of the restart matrix (skipped in `scripts/release-smoke.sh`
  with a reason, tracked in BL590 phase 2 alongside the `planning-queued`
  and `planning-decompose` rows) still does not implement the decision.
  Until both are done and observed on a real deploy, every claim in this
  draft about the *new* behavior is against the implemented-not-yet-
  live-verified state, and both sibling drafts' "can be verified only once
  that story is live-confirmed" caveat carries forward to this one as its
  own.
- **Native clients have no view of any of the three new outcome values, the
  `planning_restarted_once` field, or the policy-dependent restart outcome.**
  An Android or iPhone client still renders the `draft` reset as the
  expected loss-signal, flags the bounded auto-replan as a second planner
  session, and would surface an unrecognized `boot_resume` outcome as
  unknown/error if it switches on that value exhaustively. The PWA and the
  docs do the right thing; the native client does not.

## Expected behavior

Once parity is met, a datawatch-app client on Android and iPhone can do what
the PWA can for the *delivered* behavior, which is the union of both sibling
drafts' expected-behavior lists and the three facts listed above:

- Show a `planning` PRD as `planning` — not `draft`, not stuck, not failed —
  across a daemon restart, and watch it land at `needs_review` with a working
  approve / request-revision pair, under the `reattach` policy or an unset
  one, with the second planner session (if one appears) read as the expected
  bounded auto-replan and the `planning_restarted_once` field present when
  that happened.
- Render all six `boot_resume` outcome values — `resumed`, `parked`,
  `resumed_by_operator`, `planning_ingested`, `planning_restarted`,
  `planning_reset_to_draft` — as valid states, with each new one's plain
  `reason` text shown as-is where the surface shows the report, and an
  unrecognized future outcome (any string outside this list) still read as
  "unrecognized" rather than an error, rather than as a broken PRD. The
  planner-ingested case must show as "the plan recovered from the output
  file and the Automaton moved on", not as "planning ran twice" and not as
  "planning lost a plan".
- Carry the *effective* `planning_restart_policy` value into its
  restart-behavior copy, as the 2026-11-09 policy sibling already demands,
  but with the "until the restart path reads the setting" caveat struck from
  the copy on any build carrying this story: the daemon **does** run every
  value at boot, and the "plan reset to draft, re-queue with
  `datawatch autonomous prd-plan <id>`" guidance is the expected outcome
  only under the `draft` policy or on a build without the boot path — not
  the default for all restarts.
- Retire the old-copy "planning was lost — re-plan by hand" expected-outcome
  language the 2026-11-08 sibling flagged, but only for builds carrying the
  boot path; the old-build diagnostic stays valid verbatim and must remain
  available for the operator on any build that does not yet carry this story.

In the absence of all of that, a native client must at minimum not show
`draft` as the expected outcome for a new-build restart under the `reattach`
default, and not surface an unrecognized `boot_resume` outcome value as a
broken or failed PRD.

## Suggested next step

File one issue in **dmz006/datawatch-app** covering, for Android and iPhone
with capability parity on both platforms and no implementation parity
required (datawatch-app ships this natively): the *Planning restart policy*
control or its read in the autonomous settings (as the 2026-11-09 policy
sibling already demands, with the "until the restart path reads the setting"
caveat struck); the `planning`-across-restart render, the bounded
auto-replan acceptance, the restart-interruption count's new meaning for
planner sessions, and the old-copy retirement the 2026-11-08 sibling already
demands; and **new to this draft**, the three new `boot_resume` outcome
values as valid render states with their reason text surfaced, the
`planning_restarted_once` field read as the one-bounded-restart marker (not
a second-planner-session bug), and the *`attach_reader` outcome having no
report entry at all* as the expected "no visible change, planner still
running" state. Reference all three drafts as the source of truth for
wording and shape — `2026-11-08-daemon-restart-planning-survival-parity.md`
for the boot-path behavior, `2026-11-09-planning-restart-policy-parity.md`
for the setting and its surfaces, and this file for the three new facts and
the superseded caveats — with `docs/howto/restart-with-work-running.md`
(policy values and where to set them), `docs/operations.md` (*Restart
safety*, including the *Planning not restored after daemon restart*
troubleshooting entry, the *Still to review (operator)* block, and the
`boot_resume` report description), and `docs/datawatch-definitions.md`
(*Planning restart behavior*) as the operator-facing references. Acceptance
criteria (seeded daemon carrying this story): a PRD in `planning` at the
restart — under `reattach` (default) — stays `planning` on screen until it
lands at `needs_review`, with a working approve / request-revision pair; a
PRD that restarted once shows the `planning_restarted` entry in the
boot-resume report and the `planning_restarted_once` flag set, with no
duplicate-session flag raised; a PRD whose planner ingested shows the
`planning_ingested` entry and its reason text, not "planning ran twice";
and a PRD under the `draft` policy resets to `draft` with the old-copy
troubleshooting entry — not the new-behavior copy — as the expected-outcome
language. Carry the honest caveat in the issue body: the boot path is
implemented in this tree but not yet live-confirmed on a real deploy-restart,
and the `planning` row of the restart matrix still skips — so the issue is
filed *against the implemented behavior* and can be verified end-to-end only
once the live check lands and the matrix state enforces the decision.

## Parity surface

| Surface | Decision | Why |
|---------|----------|----|
| REST | included, delivered | No new endpoint: the existing PRD endpoints now carry `planner_session_id`, `planner_output_file` (both `omitempty`) and `planning_restarted_once` on a PRD, and `GET /api/autonomous/status`'s `boot_resume` array now includes the `planning_ingested`, `planning_restarted` and `planning_reset_to_draft` outcome values alongside the existing `resumed` / `parked` / `resumed_by_operator`. A client parsing those two payloads must accept the new values and the three new fields. |
| MCP | included, delivered | The autonomous MCP tools surface the same PRD object and the same boot-resume entries — the changed world is data-flow, not tool contract; pass-through must not flag the new outcome values or PRD fields as errors. `autonomous_config_set` (the sibling policy draft's surface) is unchanged by this story. |
| CLI | included, delivered | `datawatch autonomous status` prints the new `boot_resume` outcome values as plain strings — the CLI already renders them as-is, but a client reading that output must not parse `planning_restarted` or `planning_restarted_once` as a duplicate-session or error state. `datawatch autonomous prd-plan <id>` remains the manual requeue fallback under the `draft` policy and on builds without the boot path. No CLI behavior this work changes. |
| Comm channel | included, delivered | `autonomous status` on the comm channel and the loop-state JSON carry the same `boot_resume` entries and the same PRD fields; any comm-surface copy keyed on "planning lost at restart" retires on builds carrying the boot path. |
| YAML/config | included, delivered (by the sibling) | `autonomous.planning_restart_policy` is the knob this story reads at restart; it was already delivered on every write surface by the 2026-11-09 policy sibling, and this story does not add a setting. Noted so the issue drafter does not invent a new one. |
| PWA | included, delivered | The Autonomata view and the boot-resume report on the status page already render the new outcome values and the `planning_restarted_once` field as read-only facts; the operator-facing copy for a restarted daemon lives in `docs/howto/restart-with-work-running.md` (policy values and current status) and `docs/operations.md` (*Restart safety*, including the *Planning not restored after daemon restart* troubleshooting entry), both carrying the "live check still owed" caveat. |
| Android | excluded (tracked by this draft) | No app code lives in this repo. Capability parity is required and is tracked by this draft: the `planning`-across-restart render, the bounded auto-replan not flagged as a duplicate, the `planning_restarted_once` marker, the three new `boot_resume` outcome values as valid render states with reason text, the policy-aware restart-behavior copy with the "until the restart path reads the setting" caveat struck, and the old-copy retirement for new builds. Implementation parity is not required; datawatch-app ships this natively. |
| iPhone/iOS | excluded (tracked by this draft) | Same as Android: no code in this repo, and the same items are tracked here for the native client. |
