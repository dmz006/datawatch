# [sync] `autonomous.planning_restart_policy` gained an operator knob — parity draft for the datawatch-app native clients (Android, iPhone)

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI (no `gh`, no
issue creation on a tracker) and creates nothing on any host.

**World this work changes** (found 2026-11-08): a deploy restart killed four
planners in flight. While an Automaton was planning (decompose running), the
daemon's in-memory reader was the only thing standing between the plan and the
ground; when the daemon stopped — a deploy, an update, a crash — the boot path
reset the Automaton to `draft` and the operator had to requeue planning by
hand. The decision was recorded in
[`docs/plans/restart-matrix-findings.md`](../restart-matrix-findings.md):
planning must survive a restart. The sibling draft
[`2026-11-08-daemon-restart-planning-survival-parity.md`](2026-11-08-daemon-restart-planning-survival-parity.md)
tracks the client side of the *boot path itself*. This draft tracks the other
half of that decision: the setting the boot path reads,
`autonomous.planning_restart_policy`, which this story added to every one of
datawatch's own operator surfaces.

## Affected area

The single new daemon setting `autonomous.planning_restart_policy` and its
reader/writer surfaces. The setting answers one question — *what does a daemon
restart do to an Automaton that is still planning (its decompose running)* —
and takes exactly three values, as implemented in
`internal/config/planning_restart_policy.go`:

- `reattach` — the default, and what an unset or unrecognised value reads as:
  planning keeps running across the restart and the decompose is re-attached
  to the restarted daemon.
- `restart` — planning restarts once after the restart and a decision records
  it.
- `draft` — the Automaton is reset to draft, exactly as the daemon did before
  this setting existed.

This story delivered every datawatch-owned surface in one pass:

- **YAML/config** — the key lives in the `autonomous` block
  (`internal/config/config.go`, documented in
  [`docs/config-reference.yaml`](../../config-reference.yaml)). An unset value
  is left unset and silently reads as `reattach` at read time; an unrecognised
  value is warned about at startup and still reads as `reattach`.
- **REST** — the autonomous config read surface in `internal/server/api.go`
  reports the *effective* value (normalised through
  `NormalizePlanningRestartPolicy`, so unset reads as `reattach` rather than
  being absent), and `PUT /api/config` refuses a value that is not one of the
  three before anything is written (`validateRestartPolicyPatch`).
- **MCP** — the autonomous config tools in
  `internal/mcp/autonomous.go` and
  `internal/mcp/planning_restart_policy_config.go` expose the key as an
  enum-validated option; an invalid value is refused before anything is sent,
  and the key rides its own validated patch on `PUT /api/config`.
- **CLI** — `datawatch autonomous config-set
  '{"planning_restart_policy":"restart"}'`: the CLI refuses a bad value
  locally before anything is sent, then writes the key on a separate
  `PUT /api/config` request and prints the value it stored.
- **comm channel** — `configure autonomous.planning_restart_policy=<value>`
  carries a refusal reply that names the three accepted values when the value
  is not one of them (`internal/router/planning_restart_policy_configure.go`),
  and the `configure` help text in both router surfaces lists the key and its
  allowed values.
- **PWA** — a select field, *Planning restart policy*, in the autonomous
  settings (`internal/server/web/app.js`), with a help text in all five locales
  (en, de, es, fr, ja) that states all three values, their meanings, the
  default, and the honest caveat: until the restart path reads the setting,
  every value acts like `draft`. The locale-guard test keeps the two new keys
  locked.

What this work deliberately does **not** include: the restart path itself.
Nothing under the autonomous engine reads the policy yet — it is stored,
readable and writable everywhere an operator can reach a setting, and the
sibling draft's boot path is what will eventually act on it. That is why the
PWA help text carries the "until the restart path reads this setting" caveat,
and why this draft, like its sibling, is filed *against the decided behavior*.

## Current behavior (where the surface parity this story closes stands, and where the gap is)

On every datawatch-owned surface, an operator can now both **read the
effective policy** — unset and unrecognised both report as `reattach`, so the
read surface never hands out a value the daemon would not actually use — and
**write one of the three values**, with an unrecognised value refused at write
time on REST, MCP, CLI and the comm channel (the only place an unrecognised
value is accepted is a stale config file, which is warned about at startup and
read as the default, so a bad file can never silently pick `draft` or
`restart`). A client parsing the autonomous config can therefore trust a
closed three-value vocabulary with a defined default, and a client refusing
writes can rely on the same vocabulary from the server as from its own
pre-flight check.

The gap this work leaves is the same kind of gap as the sibling draft's, but
about the knob instead of the behavior: the operator-facing native clients in
datawatch-app have no view of `planning_restart_policy` at all. Concretely —

1. An Android or iPhone client showing the autonomous settings does not see
   the field the PWA now shows, so an operator cannot confirm *what the daemon
   will do to a planning Automaton across the next restart* from the phone —
   the very question this decision was made to answer.
2. The sibling draft records for datawatch-app a YAML/config row of *"No new
   setting: the boot path is a behavior, not a knob"*. That row is now stale:
   the knob exists, and any app copy or planning card keyed on "a restart
   resets a planning Automaton to draft, then re-plan by hand" is correct
   only while the effective policy is `draft` — it must read the policy, not
   assume it.
3. A native client that renders the *Planning not restored after daemon
   restart (PRD was mid-decompose)* troubleshooting card (quoted verbatim in
   [`docs/operations.md`](../../operations.md)) is making a promise about the
   restart outcome that the operator may now have deliberately changed — the
   card must say "under the `draft` policy (or on a build without the boot
   path)", not "always".

The setting is daemon-wide, not per-Automaton: there is no per-PRD override
surface in this story, so a client must not model it as one.

## Expected behavior

Once parity is met, a datawatch-app client on Android and iPhone can do what
the PWA can:

- show `autonomous.planning_restart_policy` in the autonomous settings as a
  three-choice control (`reattach` / `restart` / `draft`) with the same
  meaning copy as the PWA help text, including the "until the restart path
  reads the setting, every value acts like `draft`" caveat while that stays
  true;
- read back the *effective* value (unset reporting as `reattach`), so the
  settings screen never shows a blank or a fourth value;
- refuse or flag an attempt to save any value outside the three, matching the
  server's refusal, since the daemon will refuse it anyway;
- carry the policy into its restart-behavior copy: the "plan reset to draft
  after the restart, re-queue with
  `datawatch autonomous prd-plan <id>`" guidance appears as the expected
  outcome only when the effective policy is `draft` or the build in use does
  not yet carry the boot path — with the policy at `reattach` (the default),
  the expected-outcome copy the sibling draft demands applies instead
  (`planning` survives, one bounded auto-replan at most, then `needs_review`
  with the ordinary review controls);
- treat one bounded auto-replan under the `restart` policy as the normal
  outcome of that policy, not as a duplicate-session anomaly.

In the absence of a settings control, a client must at minimum *not* show a
restart-behavior promise that contradicts the effective policy it can read,
and must not surface a fourth value or an "unset shows nothing" state for a
key whose read surface always reports a value.

## Suggested next step

File one issue in **dmz006/datawatch-app** (the companion repository, per the
merge pipeline's post-ship `parity_issue` hook) covering, for Android and
iPhone with capability parity on both platforms and no implementation parity
required: the *Planning restart policy* control (or at least its read) in the
autonomous settings; the effective-value read (defaulting to `reattach`);
write refusal for out-of-vocabulary values; and the policy-aware
restart-behavior copy described in *Expected behavior*, closing out item 2 of
the gap above by explicitly amending the sibling draft's stale *"No new
setting"* YAML/config row. Reference this draft and
[`2026-11-08-daemon-restart-planning-survival-parity.md`](2026-11-08-daemon-restart-planning-survival-parity.md)
as the source of truth for wording and shape, with
[`docs/howto/restart-with-work-running.md`](../../howto/restart-with-work-running.md)
(when to pick each policy), the setting's entry in
[`docs/config-reference.yaml`](../../config-reference.yaml), and the
restart-safety section of
[`docs/operations.md`](../../operations.md) as the operator-facing
references. Acceptance criteria: seeded daemon with the key unset — the phone
shows `reattach` as the effective value; set to `restart` from the phone —
the daemon accepts it and the read surface echoes `restart`; set to any other
string — refused on the phone and rejected by the server alike; and the
restart-behavior card shown to the operator matches the effective policy on
both platforms. Carry the honest caveat in the issue body: the restart path
that acts on this setting is decided and in flight (the sibling draft's story
on Automaton `af1e0001`), so the issue is filed *against the decided
behavior* and the "until the restart path reads the setting" caveat applies
until that story is live-confirmed on a real deploy-restart.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included, delivered | Read surface reports the effective value (unset/unrecognised normalise to `reattach`); `PUT /api/config` refuses out-of-vocabulary values. No new endpoint — the key joins the existing autonomous config read and the daemon-config write. |
| MCP | included, delivered | Autonomous config tools expose the key as a three-value enum; invalid values refused before anything is sent; the key rides its own validated `PUT /api/config` patch. |
| CLI | included, delivered | `datawatch autonomous config-set` accepts the key, refuses a bad value locally, writes it on its own `PUT /api/config` request and prints the stored value; `datawatch autonomous prd-plan <id>` remains the manual requeue fallback under the `draft` policy and on old builds. |
| comm channel | included, delivered | `configure autonomous.planning_restart_policy=<value>` is refused with a reply naming the three accepted values when the value is not one of them; the `configure` help lists the key and its allowed values. |
| YAML/config | included, delivered | `autonomous.planning_restart_policy` in the config block, documented in `docs/config-reference.yaml`; unset reads as the `reattach` default, unrecognised values are startup-warned and read as the default. This row deliberately *amends* the sibling draft's "No new setting" claim on the app side. |
| PWA | included, delivered | Select field in autonomous settings with all three values, the `reattach` default, the three-locales-plus-two help text (en/de/es/fr/ja) stating meaning and the "until the restart path reads the setting" caveat; locale-guard test locks the two new keys. |
| Android | excluded (tracked by this draft) | No app code in this repo. Capability parity required: effective-value read, three-value control or refusal, policy-aware restart-behavior copy — implementation parity not; datawatch-app ships it natively. |
| iPhone/iOS | excluded (tracked by this draft) | Same as Android: no code in this repo, same items tracked here for the native client. |
