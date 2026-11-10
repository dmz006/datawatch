# Parity issue draft — the decomposer's standard tasks, shown under the Automata page's Templates tab (and never called "templates")

**Filed 2026-11-09 as dmz006/datawatch-app#425, one consolidated issue for the standard tasks drafts** (shipped in v9.123.0).

**Draft, not filed.** This is the tracked draft; the issue is opened against
`dmz006/datawatch-app` when the operator files it. Writing this file changes
nothing on any host, and filing it never runs a hosting CLI or creates anything
on a tracker. It is the follow-on that
`standard-tasks-templates-tab.md` (the wording release) named as its optional
surface; the PWA work described below implements that surface, and this draft
is the standing record of the gap it leaves for the Android and iPhone
clients.

Parent work (datawatch PWA): "Show the built-in tasks the decomposer adds to
every story in the Automata page, under the Templates tab, and stop calling
them templates" — the commits that split the Automata page's Templates tab
into the *Automaton templates* and *Standard tasks* sub-tabs
(`internal/server/web/app.js`, the `automata_templates_subtab` persisted
selection and the `_AUTOMATA_TEMPLATE_SUBTABS` registry), added the
Standard-tasks renderer that reads
`GET /api/autonomous/standard-tasks` and draws a read-only table, added the
empty state and the phone-width layout
(`internal/server/web/style.css`, `.automata-templates-subtabs`,
`.automata-templates-body`, the stacked `.automata-standard-tasks` rows), and
added the fourteen `automata_std_*`, `automata_empty_std_tasks` and
`automata_templates_subtab_*` locale keys
to all five PWA bundles (EN/DE/ES/FR/JA). Exercised by
`internal/server/web/app-automation-templates-subtabs.test.js` and
`internal/server/web/app-automation-standard-tasks.test.js`.

## Affected area

The app's Automata page, specifically the Templates tab (or the equivalent
screen wherever the app shows the Automaton definitions), on both the Android
and iPhone clients. Concretely: the templates list and its create/edit/launch
actions, and — where this gap is closed — a read-only *Standard tasks*
section beside it, reading the same daemon read the PWA now uses. The two
wording families are already decided upstream: *Automaton template* for the
reusable definitions, *standard task(s)* for the decomposer's built-in
per-story tasks, and the setting keys keep the code-level name
`autonomous.task_templates.*` behind operator-facing "Standard tasks" text.
No daemon endpoint or payload changes are involved; this is a surface and
wording gap, not a contract gap.

## Current behavior

The daemon exposes the resolved standard-task list as a read on four
surfaces — REST `GET /api/autonomous/standard-tasks` (optional
`?project=<dir-or-alias>`), the `autonomous_standard_tasks` MCP tool,
`datawatch autonomous standard-tasks`, and the chat word `standard tasks` —
and the PWA now shows it too: the Automata page's Templates tab carries a
*Standard tasks* sub-tab (beside *Automaton templates*, which is the default
and whose selection persists under `automata_templates_subtab`) that renders
one read-only row per task — Title, Kind, Summary, When it is offered, On or
off, Mode — with the fixed-mode note pointing at the
`autonomous.task_templates` setting and the standard-tasks how-to, an
explanatory empty state instead of an empty table, and a plain
"Load failed: …" line on load failure — never a stale list.

Every value is escaped, and at phone width the table becomes
stacked rows with each cell carrying its column name. The two
operator-facing limits on this surface are flagged in the how-to's
"Open items to check" section (`docs/howto/automata-standard-tasks.md`)
and belong on the gap record too. First, the standard-task **mode**
setting is not available anywhere: every task reports `model session`
and no config changes it, so no client may offer a mode control. Second,
the web sub-tab reads the **global** list: the per-project resolved read
(`?project=`) exists only on REST, MCP, CLI and the comm channel, so a
client with a notion of the active project can show the resolved state
even though the PWA does not yet.

The app has none of this. Its Templates surface shows only the Automaton
template store, and it has no place that shows which built-in tasks the
decomposer will append to every story it decomposes, or whether they are on
or off for a project. Until the wording release, its wording also could not
tell the two things apart — anything called a "template" was readable as
either — and even after it, the standard tasks remain invisible there,
discoverable only by reading the datawatch how-to, asking chat, or running the
CLI or MCP surface. The operator cannot see, on a phone, what a decompose
will add.

## Expected behavior

The app's Automata page gains a *Standard tasks* surface beside its existing
Automaton templates list, matching the PWA's sub-tab wording and content. It
reads `GET /api/autonomous/standard-tasks` — with `?project=` when the client
knows the active project — and shows each task as a read-only row: title,
kind, summary, when it is offered, its effective on/off state, and its mode,
which is fixed to `model session` today and stays that way. It is a read:
there is no create, edit or delete control on this surface and it must not
pretend there is one; the only switch that exists is
`autonomous.task_templates.enabled` (plus per-project overrides) in the
daemon's config, which the app surfaces under Settings if it surfaces
decompose-lint settings at all, labelled *Standard tasks* — and equally, no
mode control: the mode is fixed at `model session` today and there is no
per-task or per-project mode setting to wire one to. Gating follows the
PWA: when the read fails, or the daemon reports autonomous disabled, the
surface shows a plain error line (or hides the content) rather than a stale
list; when the body's `enabled` is false or `tasks` is empty, it shows a
plain empty state ("no standard tasks yet") and never an error. Every operator-facing string says
*standard task(s)* for these rows and *Automaton template* for the template
store — never "template task(s)" — and the app's row content should match
what the PWA, MCP, CLI and chat surfaces report for the same daemon, including
any operator overlays in their resolved order.

## Suggested next step

File one issue against `dmz006/datawatch-app` once the PWA release carrying
this work lands, folding in the optional-surface paragraph of
`standard-tasks-templates-tab.md` so the two drafts do not both live as open
asks. The issue should name the read (`GET /api/autonomous/standard-tasks`),
the expected row content and the fixed mode, the read-only boundary, the
empty/error gating, and the wording rule above, and it should state that no
new daemon endpoint, write path, LLM call or tracker call is part of it. If
the app later grows per-project awareness, the `project_override` field is an
optional annotation to render alongside the effective on/off state, not a
separate control.

### Acceptance

- Both the Android and iPhone clients show the standard-task list on the
  Automata page's Templates surface, matching the PWA's *Standard tasks*
  sub-tab wording and row content (title, kind, summary, when it is offered,
  on/off, mode).
- The list matches `GET /api/autonomous/standard-tasks` — the same rows the
  PWA, MCP, CLI and chat report, including operator overlays in resolved
  order; a read with `?project=` renders that project's effective state.
- A failed read or a disabled autonomous subsystem shows a plain error line
  (or hides the content), never a stale list; an empty task list shows the
  empty state, not an error.
- The app offers no create/edit/delete and no mode control on these rows,
  and if it reads with `?project=` the rendered rows must agree with what
  `datawatch autonomous standard-tasks --project <dir-or-alias>` reports
  for the same project.
- The app never calls the decomposer's built-in per-story tasks "templates",
  and never offers create/edit/delete on them.

## Filing record

- Drafted 2026-11-09 as part of the standard-tasks Templates-tab work (the
  sub-tab registry, the read-only renderer, the empty states and
  phone-width layout, and the five locale bundles). Not filed: that session
  was a file-write-only task and never ran the hosting CLI.
- Updated 2026-11-09 in the story's second parity-draft round, after the
  standard-tasks how-to was brought up to the finished sub-tab behaviour
  and gained its "Open items to check" section: current-behaviour now
  matches the how-to, and the two operator-visible limits (no mode setting
  anywhere; the web tab reads the global list while per-project resolution
  is on REST/MCP/CLI/comm) are recorded here as part of the gap. Companion
  wording draft `2026-11-09-standard-tasks-templates-tab-parity.md` remains
  unfiled.
- File against `dmz006/datawatch-app` from a session that can, after the
  PWA release lands, folding in the companion draft so the two do not both
  live as open asks, and paste the issue number here.
