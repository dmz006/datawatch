# Parity issue draft — verifier, judge, and verification-script restart behavior

**Not filed yet.** This is a draft for an issue in the client repo
(dmz006/datawatch-app, same target as its siblings) capturing the parity gap
left — or closed — by the other half of the 2026-11-08 restart-safety work:
what a daemon restart does to a task that was **verifying**, to a **guardrail
judgment** that was in flight, and to a **daemon-run verification or
final-gate script**. The planning half of the same work has its own draft:
[`2026-11-09-planning-boot-resume-parity.md`](2026-11-09-planning-boot-resume-parity.md).
Together with the two 2026-11-09 predecessors it supersedes and the 2026-11-08
finding this draft is filed against, the four drafts cover the whole
"restart-safe daemon" decision and its client-facing consequences.

## What this work changed in the world

The 2026-11-08 finding (`2026-11-08-daemon-restart-planning-survival-parity.md`)
was filed in two parts: planning lost at restart, and "the other sessions the
daemon watches in memory" — the verifier, the judge, and the script. This
story implements that second part, in `internal/autonomous`
(`verifying_status.go` — the `ResolveVerifyingResume` decision and its five
outcomes, `verifier_resume_boot_test.go`, `verifier_reconcile.go`,
`judge_resume_boot_test.go`, the manager's boot-resume paths, and
`verification_script_exec.go` / `verification_script_record.go` — the detached
run with its durable record, plus the boot-pickup paths):

1. **A task that was `verifying` at the stop keeps that state.** The daemon
   takes it back in this order, stopping at the first hit: the verifier
   session is still alive → a fresh waiter is attached to that same session
   (no second verifier, no worker reset, no retry counted); the verifier ended
   while the daemon was down but left a verdict on disk → that verdict is the
   outcome (a pass completes the task, a fail goes through the normal retry
   path); nothing usable survives → exactly one new verification runs. The
   pre-fix behavior — a finished worker being re-verified once more even when
   the old verification was still in flight or had already left a verdict —
   is gone.
2. **Verification scripts, including final-gate scripts, no longer die with
   the daemon.** They run detached and keep a durable run record under the
   data dir (`verify-runs`): pid and start time, output, exit code, and a
   delivered flag. At boot the daemon waits on the run while its process is
   alive (the timeout keeps being enforced from the recorded start time, so a
   slow script still ends on time), consumes its exit code once if it ended
   while the daemon was down, or runs the script from the start once only if
   no usable record is left. An undelivered run left by an earlier attempt is
   retired first, so no version of the script is ever run or delivered twice.
   A script that is slow, or whose effects cannot be repeated, can now survive
   a restart — previously a control-group restart ended the script as a daemon
   child and the run started over from scratch.
3. **A guardrail judgment in flight (for example the completion guardrail on
   a finished Automaton) resumes the same way:** re-attach to the live
   judgment session, read the verdict it left, or judge once — a judgment is
   never run twice for the same decision. A genuinely lost verdict is a
   different case, and the manual `rejudge` tool remains the path for it.

None of this adds a surface. No new endpoint, no new PRD or task field, no
new setting, no new `boot_resume` outcome value (the report's outcome
vocabulary is still `resumed` / `parked` / `resumed_by_operator` plus the
three planning values the sibling draft covers), and no new CLI command. What
the client world sees is the *interpretation* of states it already knows:

- The task's status enum, session list, decisions, and the existing
  `verifying` state are the carriers — a `verifying` task can now **legitimately
  outlive a daemon restart and still be in `verifying`** on the next read,
  and the session list will **not** grow a second verifier or judge session
  for it.
- The visible invariants an operator (and a parity-minded client) can rely on:
  the task's `session_id` still points at the original worker after a
  restart; an adopted verifier/judge produces no new session entry; an
  undelivered run record of the same script never causes a second run.
- The *Still to review (operator)* notes in `docs/operations.md` and the
  *Verifying, judging, and verification scripts* section of
  `docs/howto/restart-with-work-running.md` carry the honest caveat that
  the live verifier/judge adoption and the script-run pickup are the parts of
  this story still owed a live restart check on the build in use.

## Current behavior (what this work delivers, and the gap it leaves or closes)

What the client world looks like once a build carrying this story is in use:

1. **A `verifying` task surviving a restart is a normal in-progress state,
   not a stuck or failed one.** Under the pre-fix behavior the same screen
   (task still `verifying`, then the verifier running again) meant the
   verification was being redone; on a build with this story the identical
   screen can mean the daemon re-attached to the verifier that was already
   judging and will consume its verdict as if the restart had not happened.
   A client that renders "still `verifying` a long time after a restart" as
   stuck/failed, or that escalates it to "the daemon lost this task," is
   wrong for new builds.
2. **One new verification in the nothing-survives case is the expected
   bounded recovery, not a retry-loop or duplicate anomaly.** Only where
   neither a live verifier session nor a leftover verdict is usable does the
   verification run once, and only once. A client that flags "the task
   verified twice" as an anomaly will flag this only when the recovery path
   was actually taken — which is exactly when two runs are the expected
   outcome, and must be explained as such rather than as a bug.
3. **A surviving verifier or judge session in the session list after a
   restart is adoption, not duplication.** The pre-fix world had no adoption:
   a live verifier session at restart was either orphaned or its verdict
   wasted, and a second verification followed. On new builds the same
   session-list entry (one verifier session per task, unchanged `session_id`
   on the task pointing at the original worker) is the *correct* end state,
   and a client that deduplicates or reassigns "two sessions for one task"
   on sight is misreading the recovery.
4. **A script that ran once and then the daemon restarted is not a
   re-run.** The run record means the operator can distinguish "the script
   that was started before the restart finished and its result was consumed"
   from "the script was started again after the restart." A client that
   counts script executions and treats the post-restart result as a second
   execution (or as the first, when it was the continuation) is off by one
   against the record.

What this story *closes* on the client side is the open half of the
2026-11-08 sibling: its acceptance list included "the in-flight verifier is
not run twice" and "a daemon-run script is not lost," both of which were
filed against a behavior that did not exist. Those items are now testable
against a real build on the daemon side; the native side of them — the
renders and copy that make the *new* behavior legible — is what this draft
tracks.

The gap this work still *leaves*:

- **The restart-matrix rows for these states still record the pre-fix
  measurement.** `docs/operations.md` (Restart safety) marks the
  `verify-session`, `verify-script`, `verify-script-mid`, and `final-gate`
  rows as "Fixed in this tree … the matrix state(s) still record the pre-fix
  measurement … until re-run on this tree," and the release smoke skips the
  corresponding rows. Until the matrix is re-run on this tree, the enforced
  evidence for all four states is unit tests only.
- **No live deploy-restart has confirmed any of it.** Both the operations
  notes and the howto's *Current status* say the live verifier/judge
  adoption and the script-run pickup are still owed a live restart check on
  the build in use. Every claim in this draft about the new behavior is
  against the implemented-not-yet-live-verified state, and the "can be
  verified only once that story is live-confirmed" caveat the planning
  sibling carries forward applies to this one verbatim.
- **The old copy is still correct for old builds.** On any build that does
  not carry this story, the verifier *is* re-run and the script *does* start
  over after a control-group restart; the pre-fix measurement is not a
  misread there. A client that ships the new-behavior copy unconditionally
  will explain old builds wrong.

## Expected behavior

Once parity is met, a datawatch-app client on Android and iPhone can do what
the PWA can for the *delivered* behavior:

- Render a task that stays `verifying` across a daemon restart as
  in-progress — not stuck, not failed, not lost — and, when it later lands at
  complete or at the retry path, explain the outcome by the recorded
  decision (verdict consumed from a live session, from a leftover file, or a
  single new verification) rather than by a flat "verification ran twice."
- Read the single new verification in the nothing-survives case as the
  expected bounded recovery, and the surviving verifier/judge session as
  adoption (one verifier per task, task `session_id` unchanged, worker not
  re-run) — not as a duplicate-session anomaly.
- Count verification-script executions against the run record's semantics
  (one start, one result, possibly consumed after a restart) rather than
  against "the daemon restarted, so the script ran again."
- Carry the build distinction into its copy: the new-behavior explanation
  above applies only on builds carrying this story; on older builds the
  pre-fix measurement (verifier re-run, script restarted from scratch)
  remains the expected outcome and must keep being explainable.

In the absence of all of that, a native client must at minimum not render a
`verifying` task that outlived a restart as stuck or failed, and not flag a
single surviving verifier session as a duplicate.

## Suggested next step

File one issue in **dmz006/datawatch-app** covering, for Android and iPhone
with capability parity on both platforms and no implementation parity
required (datawatch-app ships this natively): the `verifying`-across-restart
render and the bounded-single-verification explanation; the surviving
verifier/judge session read as adoption, not duplication; the
script-execution count aligned to the run record; and the old-behavior copy
kept available for builds that do not carry the story. Reference the three
drafts as the source of truth for wording and shape —
`2026-11-08-daemon-restart-planning-survival-parity.md` for the original
finding (its open half), `2026-11-09-planning-boot-resume-parity.md` for the
planning half and the shared live-check caveat, and this file for the
verifier/judge/script items — with `docs/howto/restart-with-work-running.md`
(*Verifying, judging, and verification scripts*, *Current status*),
`docs/operations.md` (*Restart safety* — the four matrix rows, *What a
restart does to running work*, the KillMode note, *Still to review
(operator)*), and `docs/howto/autonomous-recovery.md` (*A task still
verifying at restart*, *Re-judging a blocked Automaton whose guardrail
verdict was lost*) as the operator-facing references. Acceptance criteria
(seeded daemon carrying this story): a task in `verifying` at the restart
shows as in-progress, then lands at complete (live verifier adopted, verdict
consumed) or at the retry path (fail verdict) with no second verifier session
in the session list and the task's `session_id` still pointing at the
original worker; a task where nothing survived shows exactly one new
verification, explained as the bounded recovery; a slow or final-gate
verification script started before the restart finishes after it, with its
result consumed once and no second start, and a run record from an earlier
attempt retired without a second delivery. Carry the honest caveat in the
issue body: the behavior is implemented in this tree but the four
restart-matrix rows still record the pre-fix measurement and no live
deploy-restart has confirmed the adoption or pickup paths — so the issue is
filed *against the implemented behavior* and can be verified end-to-end only
once the matrix re-run lands and the live check is observed on a real
deploy.

## Parity surface

| Surface | Decision | Why |
|---------|----------|----|
| REST | included, delivered | No new endpoint and no new field: the task object keeps its existing `status`, `session_id`, `attempts` and decisions, and `GET /api/autonomous/status`'s `boot_resume` vocabulary gains nothing from this story. A client that already parses `verifying` accepts the delivered world — the gap is interpretive (what a post-restart `verifying` means), not contractive. |
| MCP | included, delivered | The autonomous MCP tools surface the same task and PRD objects; pass-through must not flag a surviving verifier/judge session or a consumed-after-restart verdict as an error state. |
| CLI | included, delivered | `datawatch autonomous status` and the task listings print the same states and decisions; nothing new to parse. `datawatch autonomous rejudge <id>` remains the manual path for a genuinely lost guardrail verdict — a client that offers it must frame it as "the verdict was lost," not as "the restart broke the judgment." |
| Comm channel | included, delivered | `autonomous status` on the comm channel and the loop-state JSON carry the same entries; any comm-surface copy keyed on "the verifier re-ran after a restart" is the pre-fix world and retires on builds carrying this story. |
| YAML/config | included, delivered (nothing to deliver) | This story adds no setting: the run record's location under the data dir (`verify-runs`) and the boot-pickup order are not configurable. Noted so the issue drafter does not invent a knob. |
| PWA | included, delivered | The Automata and session views already render the post-restart states as facts; the operator-facing copy lives in `docs/howto/restart-with-work-running.md` (*Verifying, judging, and verification scripts*) and `docs/operations.md` (*Restart safety*), both carrying the "live check still owed" caveat. |
| Android | excluded (tracked by this draft) | No app code lives in this repo. Capability parity is required and is tracked by this draft: the `verifying`-across-restart render, the single-surviving-verifier-as-adoption read, the script-execution count aligned to the run record, the bounded-single-verification explanation, and the old-build copy retained for builds without the story. Implementation parity is not required; datawatch-app ships this natively. |
| iPhone/iOS | excluded (tracked by this draft) | Same as Android: no code in this repo, and the same items are tracked here for the native client. |
