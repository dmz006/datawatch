<!-- Filed 2026-11-09 as https://github.com/dmz006/datawatch-app/issues/409 (release v9.114.0). -->
# [parity] First-attempt worker routing, per-attempt reason labels and the attempt-cost settings — mobile client parity (datawatch app)

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

## Affected area

The operator-facing view of an Automaton task's attempts — which worker each
attempt ran on, why, and how an operator controls that choice — on the
Android and iPhone clients. This work ("Make a failed task attempt cost as
little as possible", the 2026-11-08 measurement of the up-to-three tries a
failing task gets) landed on the daemon three new operator-visible facts.
First, a task's first attempts can now run on a deliberately cheaper worker:
the daemon classifies every task into one of four attempt kinds —
`implementation`, `docs`, `note`, `final_gate` — and the daemon-wide key
`autonomous.first_attempt` maps a kind to the backend and model that kind's
first attempts run on, so a strong worker no longer has to start for a
docs-only or note task before it is even known to be hard. Second, once the
task has failed `autonomous.first_attempt_escalate_after_failures` times
(default 1 — the first failure escalates; range 0–20) the cheap pair stops
applying and the normal worker resolution, including the failure-escalation
ladder, takes over unchanged; a pin at the task, story, PRD or Automaton
level, or a burn-guard route, always beats the pair. Third, every attempt
record now carries the backend, model and a short reason the operator can
read — "first attempt for `docs` tasks", "escalated after 1 failure",
"escalation ladder step", "operator pinned" or "default choice" — and the
row for a held task in the Automaton summary now carries the last attempt's
reason as `last_backend_reason` alongside its backend and model.
`autonomous.retry_resume_max_context_tokens` (default 150000, range
0–2000000; 0 = every retry starts a fresh session) caps how much prior
context a retry may resume. One shared validator
(`ValidateAttemptCostSettings`) refuses an unknown kind, a
`pi`/`pi-prompt`/`ollama` entry with no model, and out-of-range numbers,
naming the key each time, on every surface that accepts the keys.

The operator guidance lives in
[docs/howto/failure-escalation.md](../../howto/failure-escalation.md) (section
"Choosing the first-attempt worker"), the *First-attempt worker (attempt
cost)* entry in
[docs/datawatch-definitions.md](../../datawatch-definitions.md), and the
*Failure escalation and held tasks* section of
[docs/operations.md](../../operations.md).

## Current behavior (what this work delivers on the server and PWA)

On the daemon the cost shape is real and auditable: a docs or note task's
first attempts run on the configured cheap pair when one is set, the first
failure (by default) hands the task to the strong worker, retries resume
with at most the configured context, and every attempt the operator reviews —
in the attempt list, the held-task row, or the review report — names the
worker it ran on and why. The settings are daemon-wide config keys in the
`autonomous` block of `config.yaml`; they are not task- or Automaton-level
overrides, there is no new REST or MCP endpoint, and the failure flow itself
(attempt order, the `review_needed` hold and its report) is exactly as
before. The PWA is the reference surface for the new read-side fields: the
held-task summary row already renders `last_backend_reason` from its payload,
and the attempt records the PWA lists carry the per-attempt backend, model
and reason.

The gap this leaves is on the Android and iPhone clients. A mobile operator
who sees a task's first attempt run on a cheap worker it would not have run
on before has nothing in the client telling them that is the configured,
designed shape — the natural read of "attempt 1 on the small model, attempt
2 on the big one" is a routing accident. A held task's row now carries a
`last_backend_reason` field the client either renders or drops, and if it
drops it the operator loses exactly the sentence that explains the worker
sequence of a task they are being asked to make a decision about. And the
three new attempt-cost settings have no home in the clients' configuration
surfaces: an operator who wants to point their docs tasks at a cheap worker
is currently limited to editing the daemon's `config.yaml` by hand, with the
validator's key-naming refusals reachable only through the surfaces that
accept the keys today.

## Expected behavior

Once parity is met, a client on either platform can do what the PWA can. It
renders the held-task row's `last_backend_reason` (and the per-attempt
backend, model and reason fields on attempt records) the way it renders the
other fields of the same payload — an unknown or empty reason reads as
absent, never as an error, so pre-existing held tasks and attempts recorded
before this release still display. The client's autonomous settings surface
exposes the three attempt-cost keys — the `autonomous.first_attempt`
kind→backend/model map (with the closed set of four kinds and the model-less-
backend refusal for `pi`, `pi-prompt` and `ollama` surfaced as the validator
names it), `autonomous.first_attempt_escalate_after_failures` and
`autonomous.retry_resume_max_context_tokens` — with the same defaults and
ranges the daemon validates, so a change made from the phone is refused or
accepted by the daemon exactly as one made by hand. And a client that shows
per-attempt or per-session figures for a retrying task presents a cheap first
attempt as the intended cost shape — first attempt on the configured cheap
worker, later attempts on the strong one — not as a degraded or truncated
session.

## Suggested next step

File one `feat(autonomous):`-prefixed issue in datawatch-app covering the
three surfaces above — the `last_backend_reason` and per-attempt
backend/model/reason fields, the three attempt-cost settings, and the
cheap-first-attempt presentation note — referencing this draft and the
failure-escalation howto as the source of truth for wording, defaults and
range. Where the app already renders held-task rows and attempt records
generically from their payloads, the acceptance criteria are a pass-through
test: one held task with a `last_backend_reason` and one held task without,
a task with three attempts on two different workers, and a round-trip of
each of the three attempt-cost keys (including one intentionally invalid
value per key to confirm the validator's message, naming the key, is shown
rather than swallowed) must display correctly on both Android and iPhone.
The settings round-trip needs the daemon-side write surface for the three
keys on the client's usual configuration channel first — if the daemon
accepts them only from `config.yaml` today, say so in the issue and let the
daemon work land before the client UI. Capability parity is required on both
platforms; implementation parity is not. This is a display-and-settings
story, not an integration story: no REST or MCP contract the clients already
parse changes shape.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included | Read-side only: the held-task summary row and the attempt records on the existing autonomous status and review endpoints now carry `last_backend_reason` and per-attempt backend/model/reason fields. No new endpoint; the three settings keys are daemon-wide config, not per-PRD state. |
| MCP | included | The autonomous status tool returns the REST status body, so the new reason fields ride along unchanged; nothing to add on the tool side. |
| CLI | included | The operator reads the effective attempt-cost values and the held-task reasons from the existing autonomous status/config commands; write-side acceptance of the three keys runs through the same shared validator as every other surface. |
| comm channel | included | Same as CLI: `configure autonomous.<key>` acceptance for the three keys runs through the shared validator, which names the key on refusal; nothing comm-specific is new. |
| YAML/config | included | The `autonomous` block gained `first_attempt` (kind→backend/model), `first_attempt_escalate_after_failures` (default 1, 0–20) and `retry_resume_max_context_tokens` (default 150000, 0–2000000); read live by the executor on the next attempt. |
| PWA | included | Reference surface: held-task rows and attempt records render the new reason fields from their payloads, and the operator guide (howto, definitions, operations) is written for it. (Wiring the three settings into the PWA's settings tabs is the same story as the mobile one — tracked here, not done by this work.) |
| Android | excluded | No app code lives in this repo. Capability parity is required and is tracked by this draft: reason-field pass-through, the three settings, and the cheap-first-attempt presentation — implementation parity is not. |
| iPhone/iOS | excluded | Same as Android: no code in this repo, and the same three items are tracked here for the native client. |

## Recording

Issue number: (to be recorded by the filer)
File date: (to be recorded by the filer)
Release: datawatch next patch release (the entry is under `[Unreleased]` in `CHANGELOG.md`)
Source work: "Make a failed task attempt cost as little as possible" — PRD `fb940001`, story files `internal/config/autonomous_attempt_cost.go`, `internal/config/config.go`, `internal/autonomous/task_attempt_kind.go`, `internal/autonomous/first_attempt_select.go`, `internal/autonomous/first_attempt_escalate.go`, `internal/autonomous/attempt_backend_reason.go`, `internal/autonomous/failure_escalation_attempts.go`, `internal/autonomous/failure_escalation_views.go`, `internal/autonomous/executor.go` and their tests.
