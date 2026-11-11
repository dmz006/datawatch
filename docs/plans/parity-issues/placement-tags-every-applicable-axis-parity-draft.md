# [parity] Placement tags: full applicable axis set on every taggable session (datawatch app)

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Feature:** every session that can carry a placement (system) tag carries the
full set of placement axes that apply to it.
**Found:** 2026-11-08, from the operator's answer to the planner / supervisor
tag question.
**Related plan:** [placement auto-tags parity plan](../2026-11-03-placement-auto-tags-parity.md).
**Start-hook half:** this draft covers the REST launch-fields half of the feature; the manager-level stamp that reaches every start *and* restart path is captured in [2026-11-09-placement-start-hook-tags-parity.md](2026-11-09-placement-start-hook-tags-parity.md).
**Related story files:** `internal/server/session_launch_tags.go`,
`internal/server/session_launch_tags_test.go`,
`cmd/datawatch/launch_tags_test.go`, `internal/mcp/server.go`.

## Affected area

Placement (system) tags on sessions, across all ten built-in axes —
`compute:<node>`, `peer:<name>`, `backend:<name>`, `lane:<name>`,
`role:<name>`, `project:<name>`, `automaton:<name>`, `automation:<name>`,
`stage:<name>` and `kind:<name>` — plus any operator-defined custom axes. The
surfaces this work touches are the session start itself
(`POST /api/sessions/start`, the REST body, the MCP `start_session` tool and
`datawatch session new`), the one-shot launch sites the daemon runs (decompose
planner, verifier, guardrail judge and rejudge, supervisor loop, task worker and
auto-fix), the tag sweep that re-stamps existing sessions, and the read surfaces
that show the result: the session list, the tag browser, the session JSON's
`system_tags`, and the placement-tag settings.

The operator guidance for all of it now lives in
[docs/howto/automata-tags.md](../../howto/automata-tags.md) (the axes table,
the supervisor-role note, *Stamping, re-stamping and the startup backfill* and
*The tag sweep*), the Tag browser / placement-tags sections of
[docs/operations.md](../../operations.md), and the entries in
[docs/datawatch-definitions.md](../../datawatch-definitions.md).

## Current behavior (what this work delivers on the server and PWA)

The datawatch side is closed. A start through `POST /api/sessions/start` now
accepts the four optional launch fields — `launch_role`, `launch_kind`,
`launch_automaton` and `launch_stage` — and the validation is up front: a role
outside `worker | verifier | planner | judge | supervisor`, or a kind outside
`task | decompose | review | rejudge | auto-fix`, is refused with 400 before any
session is started. Tagging then runs for every started session, a plain start
with no launch fields included: it stamps `compute:` (the local node, so the
tag reads `compute:local`, when the session stored no node), its backend,
its lane when the backend names one and its project — and none of the role,
kind, stage, automaton or automation axes, because it records none of those
facts.

A start that does record launch facts carries every axis those facts support:
the role and kind (such as `role:planner` or `role:supervisor`), the stage, and
the Automaton and Automation the work belongs to. Tagging is best-effort after
the session already exists: a failure is logged and the session stays usable
without its tags. The one-shot sites post their own facts through the shared
one-shot body, and are pinned by test so a site silently losing its tags fails
the suite instead of showing up as untagged sessions later: the decompose
planner posts `planner | decompose` plus its Automaton, the verifier posts
`verifier | task` plus the story stage, the guardrail judge posts `judge |
review` (or `rejudge` on a re-judgment), the supervisor loop posts `supervisor`
with no Automaton, and the task worker and auto-fix carry the worker role with
the production `task | auto-fix` kind rule. An unset field is never posted —
a caller with no launch facts posts the same body it posted before this
feature existed. The operator's supervisor session gets its role from any of
three surfaces: REST `launch_role: supervisor`, the MCP `start_session` tool's
new optional `launch_role` parameter, or `datawatch session new --launch-role
supervisor`; it then carries `role:supervisor` plus the compute, backend, lane
and project tags and no automaton tag.

Existing sessions are covered by the tag sweep (`datawatch autonomous tags
sweep`, first with `--dry-run`): it rebuilds the placement of every session
that records launch facts through the same build as at launch — including the
local-node fallback — while a session with no recorded launch facts keeps the
legacy derivation (role and kind from the session name, no node fallback) and
is left unchanged rather than guessed at, and turning an axis off clears its
tag on the next re-stamp instead of rewriting it. The manager's
placement-start hook fires once per successful start, so the stamping rides the
real start paths, and a restarted session re-stamps from its record the same
way a fresh start does — keeping the role, kind, stage and automaton axes its
record holds, with no sweep needed after a restart and no tag change on a
restart that fails. The sweep also runs on its own: once after a save under
`autonomous.tags.placement.*`, and once at daemon startup after the lease
backfill (again only when the block has changed since the last sweep).

The gap this leaves is on the operator-facing clients. The app meets a wider
tag vocabulary than it has met before: sessions that carried one or two tags
now carry up to ten, and the role and kind axes each have values the client
may not know (`verifier`, `judge`, `supervisor`; `decompose`, `review`,
`rejudge`, `auto-fix`). The session start gained four optional fields the app
does not post; omitting them behaves exactly as the app's current plain start
does, so nothing in the app is broken — it is one capability behind the PWA.
Nothing here changes a REST or MCP contract the clients already parse:
`system_tags` remains the same list field and every new field on the start body
is optional. The parity work is rendering, vocabulary and the start-surface
fields, not integration.

## Expected behavior

Once parity is met, a client on any platform can do what the PWA can: render
every axis the server writes as a read-only system tag on the session, in the
same grouping the PWA uses — including `role:supervisor` on the operator's
supervisor session next to `compute:local`, and no `peer:` tag at all on local
work — and accept the full role and kind vocabularies in any tag filter or
axis shortcut, so a supervisor session and a `kind:rejudge` one-shot are
findable rather than dropped. A client that offers a session-start surface
posts `launch_role` (and the other three launch fields) only when the operator
picked a value, treats a 400 back from the server as a plain input error with
the known-vocabulary hint, and keeps behaving exactly as it does today when it
posts none of them. A client whose settings mirror the
`autonomous.tags.placement` block needs no new control — the axis switches and
templates existed before this work — and simply continues to pass the block
through. In the absence of all of that, a client must at minimum not
misrender or silently drop the new tags or the new role/kind values —
unknown-but-valid tags should read as their payload says, not as errors or
blanks.

## Suggested next step

File one `feat(session):`-prefixed issue in datawatch-app covering the three
items above — the full axis set rendered as read-only system tags on a
supervisor session, a plain start and a one-shot (verifier or judge) session;
the widened role/kind vocabulary in the client's tag filters and labels; and
`launch_role` on the client's session-start surface, with omit-when-unset
semantics and the server's 400 wording as the input-error path — referencing
this draft and the placement howto above as the source of truth for wording and
shapes. Where the app already renders `system_tags` generically, the acceptance
criteria are pass-through: one session per shape (supervisor, plain, one-shot,
federated with a `peer:` tag) must display every axis the PWA shows for the
same session on both Android and iPhone, and each role/kind value must be
selectable in the filter. A client that has no start surface at all owes only
the rendering and vocabulary half.

Before filing, run the live check: on the operator's daemon, start a plain
session and a supervisor session and confirm their system tags against the
howto's axes table (a plain start: `compute:local`, backend, lane, project and
nothing else; a supervisor start: those plus `role:supervisor`). Then run
datawatch's tag sweep with `--dry-run`, review the list of sessions that would
change, and apply it only after that list is reviewed. Record the live-check
result in the placement parity plan's open-items list; do not mark the feature
validated until that check has been run on a live daemon.

## Parity surface

| Surface | Status | Reason |
|---|---|---|
| REST | included | `POST /api/sessions/start` gains the four optional `launch_*` fields with the 400 vocabulary guard; the stamped tags ride the existing session JSON's `system_tags` list — no new endpoint. |
| MCP | included | `start_session` gained the optional `launch_role`; the tag listing and the placement config keys stay on the existing tools. |
| CLI | included | `datawatch session new --launch-role supervisor` starts the operator's supervisor session; `datawatch autonomous tags sweep --dry-run` and without the flag cover existing sessions. |
| comm channel | existing gap | The channel's session-start does not forward the `launch_*` fields; the sweep and the tag listing are reachable as before. |
| YAML/config | unchanged | The `autonomous.tags.placement` block (master switch, per-axis `enabled`, templates) is read by the same stamping path; no new key. |
| PWA | included | The session list grouping and the placement filter already render `system_tags`; the new axes appear without a new control. |
| Android | owed to the app repo | Capability parity only — rendering, vocabulary and `launch_role` on the start surface; no implementation parity, no app code in this repo. |
| iPhone/iOS | owed to the app repo | Same as Android. |

## Open questions for the operator

- Is the tag sweep the wanted rollout for existing sessions, or should the
  daemon re-stamp them automatically on the next start?