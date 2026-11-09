# [sync] A daemon restart that would interrupt work is now refused unless forced — parity draft for the datawatch-app native clients (Android, iPhone)

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI (no `gh`, no
issue creation on a tracker) and creates nothing on any host.

**World this work changes** (found 2026-11-08): a deploy restart killed four
planners in flight. The operator asked, quoted in
[`docs/plans/restart-matrix-findings.md`](../restart-matrix-findings.md): "We
should be able to do restarts when things are running. Why can't we?" The
answer shipped in two halves. The first half — planning surviving a restart
and every other watched session (verifying tasks, guardrail judges, running
tasks, live sessions) being picked back up by the restarted daemon — is
tracked by the sibling drafts
[`2026-11-08-daemon-restart-planning-survival-parity.md`](2026-11-08-daemon-restart-planning-survival-parity.md),
[`2026-11-09-planning-restart-policy-parity.md`](2026-11-09-planning-restart-policy-parity.md)
and
[`2026-11-09-planning-boot-resume-parity.md`](2026-11-09-planning-boot-resume-parity.md).
This story (`8f990014` on Automaton `af1e0001`, "Make a daemon restart safe
for planning and every other session the daemon watches in memory") is the
second half: the **restart guard** — every restart request now first answers
"a restart now would interrupt what?", refuses a non-forced restart while any
of that work is in flight, and applies a restart policy to a restart that may
proceed.

## Affected area

The outcome an operator gets from asking the daemon to restart while work is
running, and the two read surfaces that carry it to them: the refusal
itself, and a shared read-only interruption report. Concretely, a story file
— under `internal/autonomous/` (`restart_policy.go`, `manager.go`,
`restart_interruption_report_test.go`), `internal/server/` (`api.go`,
`restart_guard_test.go`), `internal/mcp/` (`server.go` and its restart-guard
file with its test), `internal/router/` (`router.go`, the comm-channel guard
and its test) and `cmd/datawatch/` (`main.go`, `restart_policy.go`, the
CLI guard and its tests) — delivers:

1. **The interruption report.** A pure build over the PRDs, the session
   list, the live guardrail judges and the configured policies: the planning
   Automata, the verifying tasks, the running judges, the running tasks
   (executing, running tests or verifying) and the live sessions a restart
   would interrupt, each with its ids and counts, the restart policy in
   force, the quiet timeout, the on-timeout value, the launch-draining
   state, and `needs_force` — set while any of them is in flight. It is
   read-only and served the same object on every read surface:
   `GET /api/autonomous/restart-interruptions`, the `restart_interruptions`
   MCP tool, `datawatch autonomous restart-interruptions` and the chat verb
   `autonomous restart-interruptions`.
2. **The guard.** Every restart request — `POST /api/restart`, the
   `restart_daemon` MCP tool, `datawatch restart`, the chat `restart` verb —
   runs the report first. While `needs_force` is set and the request carries
   no force, the restart is **refused on that surface** and nothing is
   restarted: REST answers 409 with the report, the CLI prints the report
   and exits non-zero, the MCP tool answers an error result carrying the
   report, the chat command answers the report inline. The Web UI's Restart
   button and its auto-restart after a config change send no force, so they
   land on the refusal too, and the Web UI has **no force option by design**
   (documented in the how-to's surface table).
3. **The force flag.** `POST /api/restart?force=true` (or a `{"force": true}`
   body), `datawatch restart --force`, `restart_daemon` with `force=true`,
   and `restart force` on the messaging channel. A forced restart still
   applies the policy below; the refusal only gates *unforced* restarts.
4. **The restart policy.** `autonomous.restart_policy` — `immediate` (the
   default: restart at once), `wait_quiet` (wait up to
   `autonomous.restart_quiet_timeout_seconds`, default 600, for zero running
   tasks, then proceed or abort per `autonomous.restart_on_timeout`,
   `proceed` or `abort`), and `drain` (stop new tasks from starting — a task
   not yet started waits at the launch gate — and let the ones running
   finish; bounded by a safety cap, not a setting). Whatever the policy, the
   log, the REST reply and the tool reply all carry what the restart
   interrupted.

No endpoint, tool, status value or decision kind was introduced beyond the
one read endpoint and one read tool above; the three new settings
(`restart_policy`, `restart_quiet_timeout_seconds`, `restart_on_timeout`)
sit beside the long-standing `planning_restart_policy` in the `autonomous`
block of the config. The honest caveat is in the docs: the guard is
unit-tested against fakes on every surface, but the live confirmation of
the refusal and the forced paths on a build in use is still owed (the *Still
to review (operator)* notes in `docs/operations.md`), alongside the open
`planning` row of the restart matrix its sibling drafts track.

## Current behavior (what this work delivers, and the gap it leaves or closes)

What the client world looks like once a build carrying this story is in use:

1. **A refused restart is now a normal, structured answer — not a failure.**
   `POST /api/restart`, which the PWA's Restart button (Settings → About)
   and any app-side daemon-restart action call, answers **409 with the
   interruption report** while a planning Automaton, a verifying task, a
   running judge, a running task or a live session would be interrupted. The
   PWA today catches that as `Restart failed: <message>` — a generic error
   toast over a report that names exactly what would be interrupted and how
   to force. A native client that renders the 409 as a generic error,
   retries it, or treats the daemon as broken is misreading the expected
   answer of a new build.
2. **Force is now an explicit operator choice on the API the app already
   talks to.** `POST /api/restart?force=true` or a `{"force": true}` body
   restarts past running work (still under the policy). The PWA deliberately
   offers no force — the how-to's surface table says "restart from a surface
   that can force instead". A native client is free to offer force behind a
   second, force-named confirmation, or to steer the operator to
   `datawatch restart --force`, `restart force` on the messaging channel, or
   the REST call — but it must not invent a third refusal it cannot read, and
   its refusal copy must say what would be interrupted, because that is now
   in the body.
3. **A new read surface exists before the action.**
   `GET /api/autonomous/restart-interruptions` answers "what would a restart
   interrupt **now**" without restarting: the five lists with ids and
   counts, the policy, the quiet timeout, the on-timeout value, the
   launch-draining state, and `needs_force`. A client can show that
   report — or its `needs_force` bit — in the pre-restart confirmation
   instead of the flat "All sessions will briefly disconnect."
4. **Three new settings join the `autonomous` block.**
   `autonomous.restart_policy` (`immediate` / `wait_quiet` / `drain`),
   `autonomous.restart_quiet_timeout_seconds` (default 600) and
   `autonomous.restart_on_timeout` (`proceed` / `abort`). Unlike the
   sibling `planning_restart_policy` — which the PWA's settings page already
   exposes — these three are not on the PWA settings page today; they are
   reachable on YAML, REST (`PUT /api/config`), CLI (`datawatch config
   set`) and the comm channel (`configure ...`). The operator-facing clients
   must reflect them or record an explicit exclusion with a reason in the
   plan's Parity surface section — and this draft records that tracking
   here.
5. **The copy that restarts are unsafe while work runs is stale for new
   builds.** The 2026-11-08 finding — "a deploy restart killed four
   planners in flight" — is the *pre-fix* behavior. On a build carrying this
   story and its siblings, a restart while work is running is refused with
   a report until forced, and the work that does survive a (forced or quiet)
   restart is re-attached or picked back up rather than lost. Any client
   copy that says "never restart while sessions are active — they will be
   killed" must carry the old-build framing, because on new builds the daemon
   now *tells you* what it would kill and lets you say so explicitly.

What this story *closes* on the client side is the operator's question the
2026-11-08 finding left open: "Why can't we [restart when things are
running]?" The answer is now a guard with a report and a force flag on
every surface, not a hope. The sibling drafts' acceptance work (planning
surviving the restart, the boot-resume outcomes, the policy setting) stands
on top of this: the guard decides whether the restart may happen *at
all*, and the boot path decides what the restart does *to* the work.

The gap this work still *leaves*, exactly as before and now with a sharper
edge:

- **No live deploy-restart has confirmed the refusal or the forced paths.**
  The guard is unit-tested against fakes on every surface, and the *Still to
  review (operator)* notes in `docs/operations.md` still ask for the
  live confirmation of the refusal and the forced paths on a build in use.
  Until that lands, every claim in this draft about the *new* refusal
  behavior is against the implemented-not-yet-live-verified state, and the
  sibling drafts' "can be verified only once live-confirmed" caveat carries
  forward to this one as its own.
- **Native clients have no view of any of it.** An Android or iPhone client
   whose restart action hits the new 409 renders it as a generic failure; it
   cannot read the five lists, the policy, or the `needs_force` bit; it has
   no force path and no pre-restart report; and it neither reflects the
   three new settings nor records their exclusion. The PWA, the CLI, the MCP
   tools and the comm channel do the right thing; the native client does not.

## Expected behavior

Once parity is met, a datawatch-app client on Android and iPhone can do what
the PWA plus one force-capable surface can for the *delivered* behavior:

- Handle the refused restart as a report, not an error: on a 409 from
  `POST /api/restart` show what would be interrupted — the planning
  Automata, the verifying tasks, the running judges, the running tasks and
  the live sessions, with their ids — the restart policy in force, and how
  to restart anyway; do not retry the unforced call, and do not surface the
  daemon as failed or disconnected.
- Offer the force path explicitly — a second, force-named confirmation that
  sends `force=true` — **or** steer the operator to a surface that can force
  (`datawatch restart --force`, `restart force` on the messaging channel, or
  the REST call), the way the PWA does. Either is parity; silently dropping
  the refusal is not.
- Where the client has a pre-restart confirmation (the Android app already
  asks before restarting), it may read
  `GET /api/autonomous/restart-interruptions` first and show the
  `needs_force` state and the five lists instead of the flat "all sessions
  will briefly disconnect" copy; an unreachable report (old build) falls
  back to the flat copy.
- Reflect the three new `autonomous` settings — `restart_policy` with its
  three values, `restart_quiet_timeout_seconds`, `restart_on_timeout` — in
  its config read, or record an explicit exclusion with a reason, alongside
  the sibling `planning_restart_policy` the 2026-11-09 policy sibling
  already demands.
- Retire the "restarts kill running work, avoid restarting while sessions
  are active" expected-outcome language, but only for builds carrying the
  guard; the old-behavior framing stays valid verbatim for any build that
  does not yet carry this story.

In the absence of all of that, a native client must at minimum not render
the refusal report as a generic error, and not retry an unforced restart
that the daemon has already refused.

## Suggested next step

File one issue in **dmz006/datawatch-app** covering, for Android and iPhone
with capability parity on both platforms and no implementation parity
required (datawatch-app ships this natively): the 409 refusal from
`POST /api/restart` rendered as the interruption report — the five lists
with ids, the policy in force, and the how-to-force — with no generic-error
render and no unforced retry; the force path behind a second, force-named
confirmation or a steer to a force-capable surface (the PWA's documented
choice); the optional pre-restart read of
`GET /api/autonomous/restart-interruptions` (its `needs_force` bit and the
five lists) in the existing pre-restart confirmation; the three new
`autonomous` settings reflected in the config read or excluded with a
recorded reason; and the copy retirement above. Reference the three sibling
drafts as the source of truth for the *post-restart* world —
`2026-11-08-daemon-restart-planning-survival-parity.md` for the boot-path
behavior, `2026-11-09-planning-restart-policy-parity.md` for the
`planning_restart_policy` setting and its surfaces, and
`2026-11-09-planning-boot-resume-parity.md` for the boot-resume outcomes and
the once-only restart rule — plus this file for the guard, the force flag
and the three new settings, and
`docs/howto/restart-with-work-running.md` (the *Forcing a restart past
running work* surface table and *Checking the value*), `docs/operations.md`
(*Restart guard, then policy* and *Restart policy values*, including the
*Still to review (operator)* notes) and `docs/datawatch-definitions.md`
(the restart-guard entry) as the operator-facing references. Acceptance
criteria (seeded daemon carrying this story): with a running task or a
planning Automaton in flight, the app's unforced restart shows the refusal
naming the interrupted work and the policy in force, and nothing restarts;
the forced path (if offered) restarts and the reply still names what the
restart interrupted; with nothing in flight, the restart proceeds with no
force prompt; and the `autonomous` config read shows the three new settings
(or the recorded exclusion). Carry the honest caveat in the issue body: the
guard is implemented in this tree and unit-tested against fakes on every
surface, but the live confirmation of the refusal and the forced paths on a
build in use is still owed and the `planning` row of the restart matrix
still skips — so the issue is filed *against the implemented behavior* and
can be verified end-to-end only once the live check lands.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included, delivered | `POST /api/restart` now answers 409 with the interruption report while a non-forced restart would interrupt work, and accepts force via `?force=true` or a `{"force": true}` body; `GET /api/autonomous/restart-interruptions` is the shared read-only report (the five lists with ids and counts, the policy, the quiet timeout, the on-timeout value, the launch-draining state, `needs_force`). A client parsing those two payloads must accept the 409 body and the report object. |
| MCP | included, delivered | `restart_daemon` gains a `force` boolean and refuses with the report in the error result while `needs_force` is set and force is not passed; the new `restart_interruptions` tool reads the same report. Pass-through must not flag the refusal as a tool failure. |
| CLI | included, delivered | `datawatch restart` prints the report and exits non-zero while work would be interrupted; `datawatch restart --force` forces past it; `datawatch autonomous restart-interruptions` reads the report. The CLI already renders the refusal as prose plus report, and a client reading that output must not treat the non-zero exit as a crash. |
| Comm channel | included, delivered | `restart` answers with the report inline while work would be interrupted; `restart force` forces past it; `autonomous restart-interruptions` reads the report. Any comm-surface copy keyed on "restart always succeeds" is wrong for new builds. |
| YAML/config | included, delivered | `autonomous.restart_policy` (`immediate` default, `wait_quiet`, `drain`), `autonomous.restart_quiet_timeout_seconds` (default 600) and `autonomous.restart_on_timeout` (`proceed` / `abort`) are new settings beside `autonomous.planning_restart_policy`; they are reachable on YAML, REST (`PUT /api/config`), CLI (`datawatch config set`) and the comm channel (`configure ...`), but not on the PWA settings page. The operator-facing clients must reflect them or record an explicit exclusion with a reason. |
| PWA | included, delivered (with a documented exclusion) | The Restart button (Settings → About) and the auto-restart after a config change send `POST /api/restart` without force, so while work is in flight they report the refusal as a failed restart; the Web UI has no force option by design ("restart from a surface that can force instead", per the how-to's surface table). The 409 body is the report; the PWA renders it in the error toast rather than as the structured refusal. |
| Android | excluded (tracked by this draft) | No app code lives in this repo. Capability parity is required and is tracked by this draft: the 409 refusal rendered as the report (five lists, policy, how-to-force) with no generic-error render and no unforced retry; the force path behind a second force-named confirmation or a steer to a force-capable surface; the optional pre-restart `needs_force` read; the three new `autonomous` settings reflected or excluded with a reason; and the "restarts kill running work" copy retirement for new builds. Implementation parity is not required; datawatch-app ships this natively. |
| iPhone/iOS | excluded (tracked by this draft) | Same as Android: no code in this repo, and the same items are tracked here for the native client. |
