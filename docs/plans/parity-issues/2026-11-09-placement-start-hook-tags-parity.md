# [parity] Placement tags on every start path, not just REST starts (datawatch app)

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Feature:** every session that can carry a placement (system) tag carries the
full set of placement axes that apply to it.
**Found:** 2026-11-08, from the operator's answer to the planner / supervisor
tag question.
**This draft covers the start-hook half of the feature** — the manager-level
stamp that reaches every `Manager.Start` and `Manager.Restart` path — on top
of the REST launch-fields half already captured in
[placement-tags-every-applicable-axis-parity-draft.md](placement-tags-every-applicable-axis-parity-draft.md).
**Related plan:** [placement auto-tags parity plan](../2026-11-03-placement-auto-tags-parity.md).
**Related story files:** `internal/session/manager.go`,
`internal/session/placement_start_hook.go`,
`internal/session/placement_start_hook_test.go`, `cmd/datawatch/main.go`,
`cmd/datawatch/placement_start_wiring.go`,
`cmd/datawatch/placement_start_wiring_test.go`,
`internal/session/placement_restart_test.go`,
`internal/server/session_no_automaton_tag_test.go`.

## Affected area

The placement (system) tags stamped on sessions that are started or
restarted through paths other than `POST /api/sessions/start`. That is:
the session manager's start hook, which the daemon installs once and which
fires after every successful `Manager.Start` — including subprocess
(one-shot) starts, comm-channel starts and scheduled starts — and after
every successful `Manager.Restart`, so `RestartSession` is covered the same
way. The hook derives the session's placement from its stored record with
the same builder and the same `local` node fallback as the REST start path,
fills the lane from the backend family when the record names none, and
applies the tags under the live `autonomous.tags.placement` block, so a
config edit takes effect on the very next start without a daemon restart.
The read surfaces that see the result are the session list, the tag
browser, the session JSON's `system_tags`, and — on the operator-facing
clients — the PWA, Android and iPhone session views.

The operator guidance lives in
[docs/howto/automata-tags.md](../../howto/automata-tags.md)
(*Stamping, re-stamping and the startup backfill*) and the
placement-tag sections of
[docs/operations.md](../../operations.md) and
[docs/datawatch-definitions.md](../../datawatch-definitions.md).

## Current behavior (what this work delivers)

The gap the REST half left open is closed on the daemon: starts that
carry no launch fields at all — comm-channel starts, scheduled starts,
one-shot subprocess starts — now get the same placement stamp the REST
start path applies, derived from the session record and the live
placement block: `compute:` (reading `compute:local` when the session
stored no node), its backend, its lane when the backend names one, the
peer when the work is federated, and the project — and none of the role,
kind, stage, automaton or automation axes, because a record without
launch facts records none of those. A start that does record launch facts
keeps its full axis set: a session that belongs to an Automaton carries
the automaton (and automation) axes, and one that does not carries none
of that axis's tags — a plain start and a supervisor-role start both
arrive with `compute:local` and no `automaton:` tag, which the REST tests
pin explicitly rather than leaving to the client to infer.

A restarted session is re-stamped the same way: `Manager.Restart` calls
the hook only on a successful relaunch, so a failed restart leaves the
session's existing tags untouched, while a successful one re-derives the
full set from the record — meaning a client must treat `system_tags` as
recomputable, not immutable: an axis turned off in the placement block
clears its tag on the next re-stamp, and a lane or node the record now
resolves differently changes the tag. Tag writes are best-effort after the
session already exists: a failure is logged, never returned, and the
session stays usable without its tags, so the client must never treat a
missing tag as a failure signal.

What this work closes on the parity surface: the comm-channel start no
longer shows up as an untagged outlier against the same session started
through REST — both paths now stamp from the same record through the same
builder. What it leaves: the comm channel still has no way to *express*
launch facts, so a comm-started session can never carry the role, kind,
stage, automaton or automation axes, only the axes its record supports.
And the operator-facing client debt from the companion draft stands as
written: rendering the full axis set and vocabulary is still owed by the
Android and iPhone clients.

## Expected behavior

Once parity is met, a client on any platform reads sessions the same way
the PWA does regardless of how they were started: a comm-channel,
scheduled or one-shot session displays exactly the axes its record
supports — `compute:local`, backend, lane, peer where federated, project
— with no role/kind/stage/automaton tags invented or blanked; a session
tied to an Automaton displays that axis, and one that is not (plain or
supervisor start) displays none of it, so the client must never
synthesize an `automaton:` tag it was not given. A restart is a
re-stamp, not an anomaly: after a restarted session reappears, the client
re-reads `system_tags` and renders whatever the server now writes,
including a tag that an axis switch cleared, without diffing against the
pre-restart value. A missing or partial tag set never renders as an
error state. Where a client offers a session-start surface, the
`launch_role` field and the 400 vocabulary behavior from the companion
draft still apply; a client that only observes sessions owes rendering
alone.

## Suggested next step

Fold the two deltas specific to this work into the datawatch-app issue
the companion draft already describes, so the app gets one coherent
acceptance list: (1) a session started through a surface other than the
REST start (comm channel, schedule, one-shot) displays the same tag set
the PWA shows for the same session on both Android and iPhone — for a
plain session that is `compute:local`, backend, lane and project and
nothing else, and for a session tied to an Automaton the automaton axis
on top; (2) no `automaton:` tag appears — and none is expected — on a
plain or supervisor session that belongs to no Automaton; (3) after a
session is restarted the client re-renders its `system_tags` as written,
with cleared axes simply absent. No new app API surface is needed for any
of the three: all of it is the existing `system_tags` list. Separately,
record the one genuine remaining input gap — the comm channel has no
`launch_*` forwarding, so comm-started sessions structurally lack the
role/kind/stage/automaton axes — as an open question on the same issue,
to be decided as a comm-verb extension (a datawatch-side change, not an
app-side one) if the operator ever wants launch facts expressible over a
comm channel. File nothing before the companion draft's live check (plain
and supervisor starts, then the tag sweep `--dry-run`) has been run on a
live daemon and recorded.

## Parity surface

| Surface | Status | Reason |
|---|---|---|
| REST | included | `POST /api/sessions/start` still carries the four optional `launch_*` fields; the hook path stamps starts that carry none, so every session JSON now presents a consistent `system_tags` set. |
| MCP | included | `start_session` keeps its optional `launch_role`; MCP-started sessions without it are stamped by the hook from the record. |
| CLI | included | `datawatch session new` unchanged; the hook covers CLI-started sessions the same way, and `datawatch autonomous tags sweep` remains the remediation for pre-hook sessions. |
| comm channel | gap closed, input gap remains | Comm-channel starts now arrive stamped (previously untagged outliers); the channel still cannot express `launch_role` / launch facts, so those axes are structurally absent on comm-started sessions. |
| YAML/config | unchanged | The live `autonomous.tags.placement` block is read on every start, so an axis switch takes effect on the next start or restart without a daemon restart; no new key. |
| PWA | included | `system_tags` render as before; comm/scheduled/one-shot and restarted sessions now show the same axis set as REST-started ones. |
| Android | owed to the app repo | Rendering the full axis set from any start path, no invented `automaton:` tags, and re-reading tags after a restart; no implementation parity, no app code in this repo. |
| iPhone/iOS | owed to the app repo | Same as Android. |

## Open questions for the operator

- Should the comm channel gain a way to express `launch_*` facts (a
  datawatch-side verb extension), or is the record-derived stamp the
  intended ceiling for comm-started sessions?
- Is re-stamping on every successful restart the wanted behavior, or
  should a restart preserve the session's tags as they were before the
  relaunch?
