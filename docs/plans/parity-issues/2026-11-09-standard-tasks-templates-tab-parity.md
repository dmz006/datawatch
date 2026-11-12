# [sync] Templates tab disambiguated: "Automaton templates" vs "Standard tasks" wording, new settings help key (datawatch next release)

**Filed 2026-11-09 as dmz006/datawatch-app#425, one consolidated issue for the standard tasks drafts** (shipped in v9.123.0).

Parent release: datawatch next release

## What changed in datawatch

The PWA's Automata page used to use the word "template" for two different
things: the reusable Automaton definitions an operator starts a new Automaton
from, and the built-in task definitions the decomposer appends to every story
when the plan left them out (the standard tasks). That was a trap: the
decompose-lint settings card was labelled "Task templates" even though the
switch configures standard tasks, and the Templates tab, the modals and the
clone action all said just "template"/"Template".

This release closes that in the PWA:

- **The Templates tab is split.** The Automata page's Templates tab now has
  two sub-tabs: **Automaton templates** (the template store — create, edit,
  launch, search) and **Standard tasks** (the read-only list of the built-in
  tasks the decomposer appends to a story, plus the `autonomous.task_templates`
  switches). The tab itself is labelled **Automaton templates** in EN.
- **Every template-facing label now says "Automaton template".** The
  modal titles, the empty state, the search placeholder, the FAB action, the
  wizard link, and the Automaton detail's clone actions all carry the full
  "Automaton template" wording instead of bare "template" (see the key table
  below for EN values).
- **The decompose-lint settings card says "Standard tasks".** The toggle that
  was labelled "Task templates" is now "Standard tasks", with a new help line:
  "Configures the standard tasks the decomposer adds to each story."
- **No API change.** The daemon endpoints are unchanged: the template store
  still reads on `GET /api/autonomous/templates`, the standard-task list on
  `GET /api/autonomous/standard-tasks[?project=…]`, and the switches still
  live under `autonomous.task_templates` in `GET`/`PUT /api/config`.

## What this closes for the app

- **The word "template" in the app now has exactly one meaning.** Anything
  the app shows, names or translates as a "template" refers to the template
  store — the reusable Automaton definitions behind
  `GET /api/autonomous/templates`. The decomposer's built-in per-story tasks
  are standard tasks, not templates, and their name should never appear in
  the app's template wording again.
- **No rename of app endpoints or payloads is forced.** The REST field names
  (`templates`, `task_templates`) are unchanged code-level names; this is
  wording parity, not a contract change.

## Gap this leaves for the app

- **Wording parity.** Wherever the app renders "New template", "Clone to
  Template", "Templates" for the Automaton definitions, or labels anything
  "task template(s)", it should adopt the "Automaton template"/"Standard
  task" distinction, or at minimum stop calling the standard tasks "templates".
- **Optional surface — the standard tasks.** The app has no equivalent of the
  new **Standard tasks** sub-tab today. A natural follow-on is a read-only
  list (one line per task: `id | title | kind | enabled | mode`) from
  `GET /api/autonomous/standard-tasks[?project=…]`, plus the
  `autonomous.task_templates.enabled` toggle in Settings. Not required for
  this word release; file it as the follow-on if at all.

## Locale keys affected or added

| Key | EN (new/current value) | Note |
|---|---|---|
| `automata_tab_templates` | Automaton templates | was "Templates" |
| `automata_empty_templates` | No Automaton templates yet. | was "No templates yet." |
| `automata_tmpl_confirm_delete` | Delete this Automaton template? … | was "template" |
| `automata_tmpl_created` | Automaton template created | was "template created" |
| `automata_tmpl_deleted` | Automaton template deleted | was "template deleted" |
| `automata_tmpl_launched` | Automaton created from Automaton template | was "from template" |
| `automata_tmpl_modal_create` | New Automaton template | was "New template" |
| `automata_tmpl_modal_edit` | Edit Automaton template | was "Edit template" |
| `automata_tmpl_modal_use` | Use Automaton template: %s | was "Use template" |
| `automata_tmpl_new` | New Automaton template | was "New template" |
| `automata_tmpl_new_short` | Automaton template | was "Template" |
| `automata_tmpl_saved` | Automaton template saved | was "template saved" |
| `automata_tmpl_search` | Search Automaton templates… | was "Search templates…" |
| `automata_tmpl_vars_label` | Fill in Automaton template variables: | was "template variables" |
| `automata_wizard_template_link` | Or start from an Automaton template → | was "from a template" |
| `fab_new_template` | New Automaton template | was "New template" |
| `prd_btn_clone_template` | Clone to Automaton template | was "Clone to Template" |
| `prd_btn_clone_template_title` | Save this automaton as a reusable Automaton template | was "…template" |
| `prd_edit_menu_tip` | Edit Spec / Settings / Clone to Automaton template | was "Clone to Template" |
| `prd_terminal_state_hint` | …only Clone-to-Automaton-template + Delete remain. | was "Clone-to-Template" |
| `settings_decompose_lint_task_templates` | Standard tasks | was "Task templates" |
| `settings_decompose_lint_task_templates_help` | Configures the standard tasks the decomposer adds to each story. | **new key** |

All five EN/DE/ES/FR/JA PWA bundles carry translated values for each key, so
no placeholder text lands in the PWA; the same wording above is the EN
reference the app should follow.

## Where the values are used

- `internal/server/web/app.js`: the Settings card for decompose lint renders
  the "Standard tasks" label and help line (`settings_decompose_lint_task_templates`,
  `settings_decompose_lint_task_templates_help`), and the Automata page
  templates view, wizard, FAB and PRD-menu strings render the renamed keys.
- `internal/server/web/locales/{en,de,es,fr,ja}.json`: the key tables above in
  each of the five bundles.
- `docs/howto/automata-standard-tasks.md` and
  `docs/howto/automaton-decompose-lint.md` §5: the operator-facing wording and
  the two-things table the app wording should match.

## Acceptance

- Wherever the app shows the Automaton definitions, its wording matches "Automaton
  template" (not bare "template"), consistent with the key table above.
- The app never labels the decomposer's built-in per-story tasks as "templates".
- If the optional **Standard tasks** surface is added: one line per task
  (`id | title | kind | enabled | mode`) from
  `GET /api/autonomous/standard-tasks[?project=…]`, and the
  `autonomous.task_templates.enabled` switch in Settings — no new daemon
  endpoint required for either.
- No dependency on any new daemon field: the REST contract is unchanged by
  this release.

## Filing record

Draft only — not yet filed. This work is a file write only: no `gh` call, no
issue created on any tracker. File against `dmz006/datawatch-app` when the
wording change ships, and paste the issue number here.
