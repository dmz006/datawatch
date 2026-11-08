Issue: dmz006/datawatch-app#394

Issue URL: https://github.com/dmz006/datawatch-app/issues/394

**Parity:** The Compose app mirrors the PWA (and the REST/MCP surface behind it). This issue covers the tag placement axes (`automaton`, `automation`, `stage`, `kind`) that are on by default, the placement tags (system tags) that every Automata object now carries, the tag/axis filter query parameters on the session and Automaton list endpoints, the two new read endpoints (`GET /api/sessions/tags`, `POST /api/autonomous/tags/sweep`), the two MCP tools behind them, and every new locale key.
**PWA parity rule:** datawatch-app has to reflect the same surface: settings rows, list filters, and the locale keys below.
**Related PWA plan:** `docs/plans/2026-11-06-automata-pages-redesign.md` — the tags-and-counts decision (the operator wanted the counts proper, per-project/per-Automation/per-Automaton totals, and "anything we can tag should be tagged").

## Body of the issue

The Automata tag surface was completed on the PWA side this release:

- **Placement axes on by default.** The placement-tags block (`autonomous.tags.placement`) derives a machine-managed system tag for every enabled axis. The new four — `automaton`, `automation`, `stage`, `kind` — are on by default (as was already the case for `compute`, `peer`, `backend`, `lane`, `role`, and — now finally on — `project`), each with its conventional template.
- **System tags on every taggable object.** Automata, Automations, tasks, templates, decision records, and held questions each carry a `system_tags` list derived from their own facts (project, Automation, Automaton, stage), re-derived on every save.
- **Filter by tag or by whole axis.** The sessions list (`GET /api/sessions`) and the Automaton list (`GET /api/autonomous/prds`) take repeatable `tag` parameters plus single-axis shortcuts (`project=`, `automation=`, `automaton=`, …) and an `any`/`all` match rule.
- **Tag inventory and sweep.** `GET /api/sessions/tags` lists every tag in use with its axis, value, and the number of sessions carrying it. `POST /api/autonomous/tags/sweep` applies the in-force settings to everything that already exists, with a per-kind tally and a `dry_run` preview.
- **MCP tools** `autonomous_tag_sweep` and `session_tag_list` cover the same two surfaces for agents.

The Compose app must reflect all of it — the settings rows for the axes (including the `project` axis and its template field), the tag/axis filter UI on the sessions and Automations lists, the tag inventory, and a sweep action with a dry-run confirmation.

The operator's words on this feature:

> what were the questions for the tags and for the counting? we want to have the
> counts proper and make sure it's ready for projects so we can have totals
> overall, and by project/automation/automata. and anything we can tag should
> be tagged.

The app should follow the same rule: tag axes, chips, and filter groups; per-axis counts; a sweep action; the locale keys named below.

### Settings keys and defaults

The placement block is configured under `autonomous.tags.placement`. The new four axes — and the project axis, which is on by default now — take these keys (every surface — REST `config_set`, CLI `config set`, comm's `configure`, and the PWA settings panel — reaches the same validation):

| Key | Default | Notes |
| --- | --- | --- |
| `autonomous.tags.placement.automaton.enabled` | `true` | Axis on; tag derived for every launch with an Automaton fact. |
| `autonomous.tags.placement.automaton.template` | `automaton:{automaton}` | Must contain the `{automaton}` placeholder. |
| `autonomous.tags.placement.automation.enabled` | `true` | Axis on; tag names the owning Automation. |
| `autonomous.tags.placement.automation.template` | `automation:{automation}` | Must contain the `{automation}` placeholder. |
| `autonomous.tags.placement.stage.enabled` | `true` | Axis on; tag names the stage. |
| `autonomous.tags.placement.stage.template` | `stage:{stage}` | Must contain the `{stage}` placeholder. |
| `autonomous.tags.placement.kind.enabled` | `true` | Axis on; tag names the kind label. |
| `autonomous.tags.placement.kind.template` | `kind:{kind}` | Must contain the `{kind}` placeholder. |
| `autonomous.tags.placement.project.enabled` | `true` | Project axis, on by default now. |
| `autonomous.tags.placement.project.template` | `project:{project}` | Must contain the `{project}` placeholder. |

The master switch is `autonomous.tags.placement.enabled` (default `true`). A missing fact takes no tag — an unlabeled session takes no `stage:` tag, an Automation-less launch takes no `automation:` tag.

Validation: each axis template must contain its placeholder — `autonomous.tags.placement.kind.template` must contain `{kind}`, and so on per axis. A bad value is refused on every surface (the PWA settings screen, CLI `config set`, comm's `configure`), with the axis named in the error.

### System tags on Automata objects

Every object a user tags is derived under the in-force `tags.placement` block; the operator's own assigned tags are a separate list. The object facts are:

| Object | Facts that derive its `system_tags` |
| --- | --- |
| Automaton (PRD) | itself (`automaton:<id>`), its Automation (`automation:<id>` when it is a member), its project directory's base name (`project:<name>` when it carries one). No stage, no kind. |
| Task | the Automaton's fact plus the stage its member slot carries (stage-typed Automata only). |
| Automation | itself (`automation:<id>`) and the project of its first member (when resolvable). |
| Template | the fact of the Automaton it was cloned from, when it records one. |
| Decision record | the fact of the Automaton its `prd_id` names, when that Automaton is resolvable. |
| Held question | the fact of the Automaton its `prd_id` names, when that Automaton is resolvable. |

Sessions carry the full derived set (the run axes — `compute`, `peer`, `backend`, `lane`, `role` — only apply to sessions; objects never take a run-axis tag however the block is configured). `system_tags` is machine-managed and read-only from the API's point of view: POSTing it as an operator-assigned tag is refused. An object whose facts cannot name an axis gets no tag on that axis (shown as "(none)" in the counts).

### REST: tag and axis query parameters

Sessions list (`GET /api/sessions`) and Automaton list (`GET /api/autonomous/prds`) take the tag-and-axis selection as query parameters:

| Parameter | Sessions list | Automaton list | Notes |
| --- | --- | --- | --- |
| `tag` (repeatable, comma-separable) | ✓ | ✓ (also legacy single `tag_id`) | A tag `axis:value`; own, system, and inherited tags count on a session; a bare tag is a plain filter. |
| `tag_match` | ✓ | ✓ | `any` (default) or `all`; any other value is a 400. |
| `project=` | ✓ | ✓ | Single-axis shortcut: keeps the list to that project. |
| `automation=` | ✓ | ✓ | Single-axis shortcut: keeps the list to that Automation. |
| `automaton=` | ✓ | ✓ | Single-axis shortcut: keeps the list to that Automaton. |
| `stage=`, `kind=`, `role=`, `lane=`, `compute=` | ✓ (sessions list) | — | Single-axis shortcuts that expand to their `axis:value` tag and combine with the explicit `tag` list under `tag_match`. |
| `peer=` | — | — | No shortcut parameter — the sessions list does not expand `peer=`; a `peer:<name>` tag is filterable via the `tag=` list only. |
| `backend=` | ✓ (special) | — | Not a tag shortcut: it stays the sessions list's session-backend filter, which a session satisfies by its backend family name OR by carrying the `backend:<value>` tag, and it applies independently of `tag`/`tag_match`. |

Examples:

```
GET /api/sessions?project=datawatch&stage=run&tag_match=any
GET /api/sessions?tag=kind:feature,stage:run&tag_match=all
GET /api/autonomous/prds?automaton=6f31d818
GET /api/autonomous/prds?project=datawatch
```

A bad `tag_match` is a 400:

```
tag_match: must be "any" or "all"
```

### REST: session tag inventory

```
GET /api/sessions/tags
```

Lists every tag a live or archived session carries, split into its axis and its value, counted over the sessions that carry it. A session counts once per tag — its own tags, its system tags, and the tags it inherits all count, but a tag appearing on more than one of those lists is counted once. Sorted by axis (tags without an axis last), then count (descending), then tag.

```json
{"tags": [
  {"tag": "project:datawatch",        "axis": "project",   "value": "datawatch", "count": 42},
  {"tag": "stage:run",                "axis": "stage",     "value": "run",       "count": 31},
  {"tag": "automaton:6f31d818",       "axis": "automaton", "value": "6f31d818",  "count": 18},
  {"tag": "automation:4c9a2be7",      "axis": "automation","value": "4c9a2be7",  "count": 12},
  {"tag": "kind:maintenance",         "axis": "kind",      "value": "maintenance","count": 9},
  {"tag": "lane:high",                "axis": "lane",      "value": "high",      "count": 7},
  {"tag": "wip",                      "axis": "",          "value": "wip",       "count": 3}
]}
```

Requires only the session-list capability — the request is read-only.

### REST: tag sweep

```
POST /api/autonomous/tags/sweep
POST /api/autonomous/tags/sweep?dry_run=true
```

Applies the in-force tag settings to everything that already exists: one pass over every session and record the Automata own, re-deriving each one's `system_tags` from its stored facts and writing the changed ones back. Turning an axis off removes that axis's tag from everything; turning it on adds it where the fact exists. The operator's own tags and the inherited tags are a separate field the sweep never reads or writes; a second pass changes nothing (idempotent). The `dry_run` query flag (or `{"dry_run": true}` body) runs the full re-derivation and reports the would-be counts without writing a single tag.

The reply is a per-kind tally — `sessions`, `automata`, `automations`, `tasks`, `templates`, `decision_records`, `questions` — each with `examined`, `changed`, `unchanged`, `failed`:

```json
{
  "sessions":         {"examined": 42, "changed": 5,  "unchanged": 37, "failed": 0},
  "automata":         {"examined": 18, "changed": 18, "unchanged": 0,  "failed": 0},
  "automations":      {"examined": 12, "changed": 4,  "unchanged": 8,  "failed": 0},
  "tasks":            {"examined": 61, "changed": 11, "unchanged": 50, "failed": 0},
  "templates":        {"examined": 7,  "changed": 7,  "unchanged": 0,  "failed": 0},
  "decision_records": {"examined": 23, "changed": 3,  "unchanged": 20, "failed": 0},
  "questions":        {"examined": 4,  "changed": 2,  "unchanged": 2,  "failed": 0}
}
```

Requires the autonomous-write capability; `dry_run` is a query flag (the body field is accepted on top).

### MCP tools

Two tools cover the same two surfaces for agents (the app should offer the sweep action and the tag-list read through its settings and session list):

- `autonomous_tag_sweep` — proxies `POST /api/autonomous/tags/sweep`. Single boolean argument, `dry_run`: `true` runs the full re-derivation and reports the would-be counts without writing; `false` or absent is the writing run (the default). Returns the per-kind tally.
- `session_tag_list` — read-only. Lists the tags in use across live and archived sessions, each with `tag`, `axis`, `value`, and `count`, in the inventory's sort order. No arguments.

### Locale keys

Every locale file — `en`, `de`, `es`, `fr`, `ja` — gets the new keys. The app should carry the same strings for its settings rows, help entries, session-list tag filter, and sweep action (translated into its own strings the same way it translates the neighboring rows). New keys (English reference; `de`, `es`, `fr`, `ja` carry translations of the same set):

**Settings rows and help (the four new axes + the project row's default):**

| Key | English |
| --- | --- |
| `settings_placement_group_help` *(updated)* | Placement (system) tags are derived by the machine for every session it creates — `compute:<node>`, `peer:<name>`, `backend:<llm>`, `lane:<lane>`, `role:<role>`, `automaton:<id>`, `automation:<name>`, `stage:<stage>`, `kind:<label>`, `project:<name>`. Enabling an axis adds its tag to sessions and lets the UI group and filter by tag or axis. |
| `settings_placement_automaton` | Automaton axis |
| `settings_placement_automaton_help` | Adds a tag with the id of the Automaton this launch belongs to, so the session list, dashboard and Gantt can group and filter by Automaton. |
| `settings_placement_automation` | Automation axis |
| `settings_placement_automation_help` | Adds a tag naming the Automation that owns the launch, so the session list, dashboard and Gantt can group and filter by Automation. |
| `settings_placement_stage` | stage axis |
| `settings_placement_stage_help` | Adds a tag with the stage the launch was made in, so the session list, dashboard and Gantt can group and filter by stage. |
| `settings_placement_kind` | kind axis |
| `settings_placement_kind_help` | Adds a tag with the kind of the launch, so the session list, dashboard and Gantt can group and filter by kind. |
| `settings_placement_automaton_template` | Automaton tag template |
| `settings_placement_automation_template` | Automation tag template |
| `settings_placement_stage_template` | stage tag template |
| `settings_placement_kind_template` | kind tag template |

**Session-list tag filter (toolbar chips drawn from `GET /api/sessions/tags`):**

| Key | English |
| --- | --- |
| `session_tag_filter_btn` | Tags (%1$d) |
| `session_tag_filter_tip` | Toggle the tag filter — chips grouped by axis; pick tags to narrow the list |
| `session_tag_help_tip` | Open the Automata & tags howto — tag axes, filters, and the tag browser |
| `session_tag_chip_tip` | %1$s — %2$d sessions |
| `session_tag_clear_tip` | Remove the tag filter |

## Acceptance

- The app's settings screen shows the Placement-tags group with the axis toggles and per-axis template fields for `automaton`, `automation`, `stage`, `kind`, and `project`, defaulting all five on, and the group help text names all the axes.
- The app's sessions list and Automations list support filtering by tag, by axis (`project=`, `automation=`, `automaton=` for the Automaton list; the full axis set for sessions), and by `any`/`all` match — surfaced as chips grouped by axis with per-tag session counts from the tag inventory.
- The tag inventory (axis → value → count) is available to the app for its filters and its per-project/per-Automation/per-Automaton totals; untagged objects read as "(none)".
- The sweep action is reachable from the app's settings/surface, offers a dry-run preview first, and shows the per-kind tally (examined / changed / unchanged / failed) once it runs; a second run changes nothing.
- The app carries the locale keys above for every locale it ships, matching the neighboring rows' style; the two MCP tools (`autonomous_tag_sweep`, `session_tag_list`) are documented as the agent-facing equivalents.

## Filing record

Filed 2026-11-07 as [datawatch-app#394](https://github.com/dmz006/datawatch-app/issues/394). The command used was:

```
gh issue create --repo dmz006/datawatch-app \
  --title "Parity: Automata tags — axes, counts, and the sweep (tag-everything)" \
  --body-file docs/plans/parity-issues/2026-11-06-tag-everything-parity.md
```
