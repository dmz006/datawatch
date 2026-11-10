---
docs:
  index: true
  topics: [autonomous, automaton, change record, generated artefacts, changelog, parity, testing tracker, registry, templates]
exec_params:
  - {name: id, required: true, description: "Automaton id to read the change record or an artefact for"}
  - {name: kind, required: true, description: "Artefact kind: gate_report, changelog_draft, parity_draft, tracker_rows or registry_row"}
exec_steps:
  - tool: generated_artefacts_get
    description: Read the generated-artefact switches in effect, plus each kind with its format and tracked path
    args: {}
    read_only: true
  - tool: autonomous_prd_change_record
    description: Read the change record of an Automaton (files, packages, settings, routes, locale keys, tests, commands)
    args:
      id: "{{params.id}}"
    read_only: true
  - tool: autonomous_prd_generated_artefact
    description: Preview one generated artefact without writing it
    args:
      id: "{{params.id}}"
      kind: "{{params.kind}}"
    read_only: true
  - tool: autonomous_prd_regenerate_artefact
    description: Re-render one artefact and write it to its tracked path; a file a person edited is kept
    args:
      id: "{{params.id}}"
      kind: "{{params.kind}}"
    read_only: false
---

# Generated artefacts

An Automaton records what it changed, and the daemon turns that record into
the small, repetitive documents a change needs: a gate report, a changelog
entry, a mobile parity issue, testing-tracker rows and a registry row. Nobody
plans a task for them, and nobody types them from memory. They are drafts: a
person can edit any of them, and an edited file is never overwritten.

This guide is for operators. It covers what the record holds, the five
artefacts, how to edit and override them, every setting, every surface, how
the review step reads them, and how to adopt the scheme in a new repository.

## 1. The change record

When an Automaton task finishes, the daemon builds a change record from the
task's diff and the commands the task ran, and keeps it. The records of all
tasks of an Automaton merge into one. It holds:

| Field | Meaning |
|---|---|
| `files` | Every changed file with its status (added, modified, deleted) |
| `packages` | The code packages those files belong to |
| `settings_added` | Configuration keys the change added |
| `routes_added` | REST routes the change added |
| `locale_keys_added` | New locale keys with their English values |
| `tests_added` | Names of the tests the change added |
| `commands` | Each command run, its exit code and the tests it passed and failed |

The artefacts are rendered from these fields. The record is data, not prose:
a task that adds nothing of a kind leaves that list empty and the artefact
says "None." for it.

## 2. The five artefacts

Every generated file starts with this marker line:

```
<!-- generated: datawatch change record; edit freely, a person edit is kept -->
```

| Kind | Default tracked path | Format |
|---|---|---|
| `gate_report` | `docs/plans/reports/<automaton-id>-gate.md` | Counts only |
| `changelog_draft` | `docs/plans/drafts/<automaton-id>-changelog.md` | Changelog entry draft |
| `parity_draft` | `docs/plans/parity-issues/<automaton-id>.md` | Mobile app parity issue draft |
| `tracker_rows` | `docs/plans/drafts/<automaton-id>-tracker-rows.md` | Table rows |
| `registry_row` | `docs/plans/drafts/<automaton-id>-registry-row.md` | One table row |

`<automaton-id>` is replaced with the Automaton id. The paths are relative to
the project directory, and the daemon creates the folders.

### Gate report: counts only

A table of numbers and nothing else, so a reader can see the size of the
change at a glance and no judgment hides in it:

```
# Gate report <id>

| Count | Value |
|---|---|
| Commands run | 6 |
| Commands passed | 6 |
| Commands failed | 0 |
| Tests passed | 41 |
| Tests failed | 0 |
| Files changed | 14 |
| Packages touched | 5 |
| Tests added | 12 |
| Settings added | 7 |
| Routes added | 4 |
| Locale keys added | 35 |
```

### Changelog entry draft

A `CHANGELOG.md` style entry. The heading carries the placeholder `vX.Y.Z`,
never a number, because the release version is taken when the work is merged:

```
## vX.Y.Z — Automaton <id>

Change from Automaton <id>: 14 files in 5 packages.

- 7 new settings: `autonomous.generated_artefacts.gate_report`, ….
- 4 new routes: `GET /api/…`, ….
- 35 new locale keys.
- Touches 5 packages: `internal/config`, ….
- 12 tests added.
```

A bullet appears only when its count is above zero.

### Mobile app parity draft

A draft of the issue for the mobile app, with the sections the app team
needs:

```
# [sync] Automaton <id> (datawatch next release)

Parent release: datawatch next release

## What changed
<one line summary>

## Settings to add to the mobile app
- `autonomous.generated_artefacts.gate_report`

## REST contract from routes added
- `GET /api/autonomous/generated-artefacts`

## New locale keys with English values
| Key | English |
|---|---|
| `settings_generated_gate_report` | Gate report |

## Screens
- `internal/server/web/app.js`
```

Each section reads "None." when the record has nothing for it. Locale files
are not listed as screens; their keys go in the key table. Read the draft and
add the placement and defaults a person knows; do not file it unread.

### Testing-tracker rows

One row per surface the change touched (web UI, REST API, MCP, CLI, config,
comm channel), in the column order of the table in `docs/testing-tracker.md`:
Feature, Test Name, Test Conditions, Description, Test Type, Tested?,
Pass/Fail?, Validated?, Notes. The rows come out as `Tested? Yes` when the
change added tests and `No` when it did not, `Pass/Fail? Pending` and
`Validated? No`. Validation needs a live run by a person; the draft never
claims one.

### Registry row draft

One row for the registry table in `docs/plans/README.md`:

```
| NNN | <title> | draft, version taken at merge (next patch above main) | - |
```

`NNN` is the row number, which a person fills in at merge. The status never
names a version number.

### Relationship to the daemon-mode standard tasks

The `changelog_entry` and `parity_issue_draft` standard tasks (described in
[automaton-decompose-lint.md](automaton-decompose-lint.md)) are daemon-mode
by default. When the daemon runs them as part of a story, it renders the
same artefact content from the change record and writes the file, using the
same template files and edit-protection rules described here. The
generated-artefacts system fires *after* a task finishes (event-driven);
the daemon-mode standard task fires as a story task (plan-driven). Both
routes produce the same file, and an edit to the file is protected either
way.

## 3. Editing an artefact, and why an edit is never overwritten

Open the file and edit it. That is the whole procedure. Three things make the
daemon leave a file alone:

1. The marker line is missing. A file that never had it, or one you
   rewrote from scratch, is yours.
2. The line `<!-- edited -->` follows the marker. Add it to say "I took this
   over" explicitly.
3. The body no longer matches the hash the daemon stored when it wrote the
   file (a hidden `.<file>.genhash` file next to it). Any change to the text
   counts, even one character, so an edit without the second line is still
   safe.

In each case a regenerate returns `skipped` with the reason, and the file is
untouched. A file nobody touched is rewritten in place when the record changes
(for example after a later task of the same Automaton). To hand a file back to
the daemon, delete it; the next regenerate writes it fresh.

## 4. Overriding a template

Each kind renders from a template. To use your own layout, put a Go
`text/template` file at:

```
.datawatch/templates/<kind>.tmpl
```

for example `.datawatch/templates/changelog_draft.tmpl`. The project's file wins over
the built-in one. The placeholders are listed in section 8. A missing key is
an error, not an empty string, so a typo cannot silently produce a blank.

If the override cannot be parsed, or fails when it runs, the daemon does not
fail the task: it uses the built-in template instead and returns a warning
that names the override file and the error. Fix the file and regenerate.

## 5. Settings

All settings sit under `autonomous.generated_artefacts`. Each is a boolean,
and each is on when unset. A change applies without a restart.

| Setting | Default | Turning it off |
|---|---|---|
| `change_record` | on | No change record is built, stored or merged. The other artefacts still render, but from an empty record, so their counts are zero and their lists read "None." |
| `gate_report` | on | The gate report is not written |
| `changelog_draft` | on | The changelog draft is not written |
| `parity_draft` | on | The parity draft is not written |
| `tracker_rows` | on | The tracker rows are not written |
| `registry_row` | on | The registry row is not written |
| `combined_docs_task` | on | Decompose is no longer told to plan one combined documentation task, so it may plan several documentation tasks |

Turning an artefact off also changes planning: because the daemon no longer
produces that document, the planner keeps a task for it. A disabled kind is
not a finding in review either (section 7).

## 6. The single combined documentation task

Decompose plans at most one documentation task per Automaton, for the
documentation that needs judgment: the how-to, the definition and operator
docs. Its file list covers every doc it writes. There is no documentation task
per surface.

For each enabled artefact, the decomposed plan is cleaned: a task whose whole
purpose is one of the five artefacts (for example "Draft the changelog entry"
that names no source files, or only touches `CHANGELOG.md`) is dropped, and
tasks that depended on it are re-linked. A task that also changes code, even
if it mentions the changelog, is kept. Gate and smoke tasks are never dropped.
With `combined_docs_task` off the planner is not given the one-documentation-task instruction; the dropping of artefact-only tasks still follows each artefact's own setting.

## 7. How the review step reads the artefacts

The rules review at the end of an Automaton looks at the generated files as
part of its context:

- Each enabled artefact that exists is named as a context file the reviewer
  also reads.
- A rule that asks for a changelog entry, a parity issue, tracker rows or a
  registry row is met when the matching generated artefact covers it.
- An enabled artefact that is missing is a finding, and the review lists the
  expected path.
- A kind that is switched off is not a finding.

## 8. Every surface, one example each

**REST.** Read the switches and kinds, then the record, a preview and a
regenerate:

```bash
curl -s $BASE/api/autonomous/generated-artefacts
curl -s $BASE/api/autonomous/prds/<id>/change-record
curl -s $BASE/api/autonomous/prds/<id>/generated/changelog_draft
curl -s -X POST $BASE/api/autonomous/prds/<id>/generated/changelog_draft/regenerate
curl -s -X PUT $BASE/api/config -H 'Content-Type: application/json' \
  -d '{"autonomous":{"generated_artefacts":{"gate_report":false}}}'
```

The preview writes nothing and says whether the kind is enabled. A value that
is not a boolean is a 400.

**MCP.** `generated_artefacts_get` reads the switches;
`autonomous_prd_change_record`, `autonomous_prd_generated_artefact` and
`autonomous_prd_regenerate_artefact` take `id` (and `kind`). To change a
switch use `config_set` with the key
`autonomous.generated_artefacts.registry_row` and the value `false`.

**CLI.**

```bash
datawatch autonomous generated-artefacts            # show
datawatch autonomous generated-artefacts set gate_report off
datawatch autonomous change-record <id>
datawatch autonomous generated <id> changelog_draft            # preview
datawatch autonomous generated <id> changelog_draft --regenerate
```

**Comm channel.** The same verbs as chat commands:
`autonomous generated-artefacts set parity_draft off`,
`autonomous change-record <id>` and `autonomous regenerate <id> <kind>`.

**Config file** (`~/.datawatch/config.yaml`):

```yaml
autonomous:
  generated_artefacts:
    change_record: true
    gate_report: true
    changelog_draft: true
    parity_draft: true
    tracker_rows: true
    registry_row: false     # this team keeps the registry by hand
    combined_docs_task: true
```

**Web settings.** Settings, Automata card, group "Generated artefacts": one
toggle per setting with a help line. The Automaton detail view has a
"Generated artefact" picker and a Regenerate button, and shows the change
record as a read-only summary of counts. Labels are in English, German,
Spanish, French and Japanese.

## 9. Adopting the scheme in a new repository

The daemon needs nothing installed in the repository; it writes files under
the project directory of the Automaton.

1. Make sure the default folders suit you: `docs/plans/reports/`,
   `docs/plans/drafts/` and `docs/plans/parity-issues/` are created on the
   first write. The paths are the defaults listed in section 2.
2. Check that the files an artefact is meant to feed exist where you expect
   them: `CHANGELOG.md`, `docs/testing-tracker.md` and `docs/plans/README.md`.
   The artefacts are drafts beside them; a person (or the merge step) moves a
   draft into the real file.
3. Leave all seven settings on, or turn off the kinds your project does not
   use (no mobile app: `parity_draft` off; no tracker: `tracker_rows` off).
4. Optionally add `.datawatch/templates/<kind>.tmpl` for a kind whose layout
   differs, using the placeholders below.
5. Commit the `.datawatch/templates/` folder so every worker and every
   teammate renders the same layout.
6. Run an Automaton, then read each file with the preview and check the
   rendered layout once.

Template placeholders (all kinds): `.ID`, `.Title`, `.Summary`,
`.RowNumber`, `.CommandsRun`, `.CommandsPassed`, `.CommandsFailed`,
`.TestsPassed`, `.TestsFailed`, `.FilesChanged`, `.PackagesTouched`,
`.TestsAdded`, `.SettingsAdded`, `.RoutesAdded`, `.LocaleKeysAdded`, the
lists `.Settings`, `.Routes`, `.Screens`, `.Bullets`, the locale list
`.LocaleKeys` (each with `.Key` and `.Value`) and the surface list
`.Surfaces` (each with `.Feature`, `.Tests`, `.Conditions`, `.Description`,
`.TestType`, `.Tested`). The function `cell` makes text safe inside one
table cell.

Checklist:

- [ ] Settings reviewed (section 5)
- [ ] Target files (`CHANGELOG.md`, tracker, registry) exist
- [ ] Overrides committed, if any
- [ ] One Automaton run read through the preview
- [ ] Team knows an edited draft is never overwritten
