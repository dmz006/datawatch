Issue: pending, filed at the merge

**Filed 2026-11-09 as dmz006/datawatch-app#419, one consolidated issue for all the release approval drafts** (shipped in v9.116.0).

# [sync] Release approval modes: autonomous.merge_pipeline.release_approval settings (patch, minor, major), release_approval_in_effect on the merge pipeline card, and the ask decision in the questions list

## Body of the issue

The daemon now decides what happens to a prepared release before it leaves the machine (the tag, the push and the deploy) from a setting, one mode per release size. The mode is `auto`, `ask` or `manual`. This records the exact contracts the Android and iPhone apps should render, the settings to expose, the locale keys, and how the `ask` decision shows up in the questions the app already lists.

### The modes and sizes

| Mode | What the daemon does |
|---|---|
| `auto` | Runs the checks. When they pass, it ships the release and reports afterwards. Nobody is asked first. |
| `ask` | Prepares everything, raises one yes/no question with the merge plan, and ships only after a yes. |
| `manual` | Prepares the release and reports. The operator ships it; nothing is asked. |

There are three sizes: `patch`, `minor` and `major`. The release's own versioning decides which size a release is; the setting only says what to do with each size. Defaults: patch `auto`, minor `auto`, major `ask`. A release whose size cannot be read resolves to `ask`, never to shipping. A failed check holds the release for a person in every mode and is never asked about.

For each size the mode in effect is the first one set: the project's override, then the global value, then the default.

### REST: settings

`GET` and `PUT /api/config`. `PUT` takes dotted keys and checks every key before applying any, so one bad value refuses the whole `PUT` with a message naming the key and the valid values (`auto`, `ask` or `manual`).

| Config key | Type | Default | Valid values |
|---|---|---|---|
| `autonomous.merge_pipeline.release_approval.patch` | string | `auto` | `auto`, `ask`, `manual` |
| `autonomous.merge_pipeline.release_approval.minor` | string | `auto` | `auto`, `ask`, `manual` |
| `autonomous.merge_pipeline.release_approval.major` | string | `ask` | `auto`, `ask`, `manual` |

One project's value is set under `autonomous.merge_pipeline.projects.<project>.release_approval.<size>` by the same `PUT /api/config`. An empty value or `null` for a project key clears that size back to inheriting. `GET /api/config` reads the effective block back under `autonomous.merge_pipeline.release_approval`. An absent field falls back to the default in the table.

### REST: merge pipeline read

`GET /api/autonomous/merge-pipeline?project=NAME` (same access and errors as the existing merge pipeline read) gained one field:

| Field | Type | Notes |
|---|---|---|
| `release_approval_in_effect` | object | `patch`, `minor` and `major`, each always present and always one of `auto`, `ask` or `manual`; resolved for the requested project (its override, else the global value, else the default) |

Older daemons do not send the field. When it is absent or not an object, the card shows the rows without a mode selected and does not fail.

### Behaviour of the ask decision

In `ask` mode the daemon raises one question, once per run before the first step that leaves the machine. It appears in the existing questions list (`GET /api/questions`, the same list the app already renders) with no new endpoint:

- `asked_by` is `release-approval`, the asker kind is system and the label is `decision`.
- The text is the plain-language merge plan: the project, the Automaton when there is one, the size, the version the release would get, the branches it merges, each check with passed or failed, and the steps that would run in order. It ends with `Ship this release?`.
- The two options are `yes` (labelled "yes (ship it)": the release is tagged, pushed and deployed as listed) and `no` (labelled "no (do not ship)": nothing is tagged, pushed or deployed and the release waits). The default is `no`.
- A question tied to an Automaton is linked to it, so it also shows in that Automaton's questions.
- A re-run for the same project, Automaton and size finds the open question instead of raising a second one, so the list never shows duplicates.
- The run ships only on a `yes`. Answering uses the existing question answer path (`POST /api/questions/{id}/answer` with the option id); the app needs no new control, only to render the options as it does for any other question.
- If the question cannot be raised, the run fails short of the ship.

### MCP, CLI and comm channel

- `autonomous_merge_pipeline_get` returns `release_approval_in_effect` in its JSON answer; its text answer adds a `Release approval: patch …, minor …, major …` line. The CLI (`datawatch autonomous merge-pipeline show`) and the chat verb (`autonomous merge-pipeline`) print the same line.
- `config_set` and `datawatch config set` take the dotted keys above. `autonomous_config_set` takes `merge_pipeline_release_approval_patch`, `merge_pipeline_release_approval_minor` and `merge_pipeline_release_approval_major`, with `merge_pipeline_project` to apply them to one project.
- The comm channel `configure` verb sets the dotted keys; any other mode is refused with the key named before it is stored.

### PWA (reference render)

- **Merge pipeline card** (Settings > Automation > Merge pipeline): a Release approval block with one select per size (Patch releases, Minor releases, Major releases). Each select lists the three modes and shows the one in `release_approval_in_effect`. Choosing a mode saves that project's override through `PUT /api/config` with the key `autonomous.merge_pipeline.projects.<project>.release_approval.<size>`, then reloads the card. A size or mode outside the lists is ignored before anything is sent.
- **Settings fields** in the Merge pipeline group: one select per size for the global value, each with a hint.

### Settings to add to the mobile app

| Config key | Label (English) | Type | Default | Control |
|---|---|---|---|---|
| `autonomous.merge_pipeline.release_approval.patch` | Release approval for patch releases (auto, ask or manual; default auto) | string | `auto` | select: Ship without waiting / Ask the operator first / Left for the operator to ship |
| `autonomous.merge_pipeline.release_approval.minor` | Release approval for minor releases (auto, ask or manual; default auto) | string | `auto` | same select |
| `autonomous.merge_pipeline.release_approval.major` | Release approval for major releases (auto, ask or manual; default ask) | string | `ask` | same select |

A major release is the operator's decision by default. Moving it to `auto` is a deliberate choice to record with its evidence; the hint says so.

### New locale keys

The keys below were added to the five web bundles (`en`, `de`, `es`, `fr`, `ja` under `internal/server/web/locales/`). The `de`, `es`, `fr` and `ja` bundles carry the English text as a placeholder until the translations arrive.

#### Merge pipeline card (7 keys)

| Key | EN value | Context |
|---|---|---|
| `merge_pipeline_release_approval_label` | Release approval | card text |
| `merge_pipeline_release_approval_patch` | Patch releases | card text |
| `merge_pipeline_release_approval_minor` | Minor releases | card text |
| `merge_pipeline_release_approval_major` | Major releases | card text |
| `merge_pipeline_release_approval_mode_auto` | Ship without waiting | select option |
| `merge_pipeline_release_approval_mode_ask` | Ask the operator first | select option |
| `merge_pipeline_release_approval_mode_manual` | Left for the operator to ship | select option |

#### Settings (6 keys)

| Key | EN value | Context |
|---|---|---|
| `settings_merge_pipeline_release_approval_patch` | Release approval for patch releases (auto, ask or manual; default auto) | settings label |
| `settings_merge_pipeline_release_approval_patch_hint` | What happens before a patch release ships. auto ships it without waiting, ask asks the operator first, manual leaves the release for the operator to ship. A project can set its own mode on the merge pipeline card. | settings hint |
| `settings_merge_pipeline_release_approval_minor` | Release approval for minor releases (auto, ask or manual; default auto) | settings label |
| `settings_merge_pipeline_release_approval_minor_hint` | What happens before a minor release ships, with the same three modes as patch releases. A project can set its own mode on the merge pipeline card. | settings hint |
| `settings_merge_pipeline_release_approval_major` | Release approval for major releases (auto, ask or manual; default ask) | settings label |
| `settings_merge_pipeline_release_approval_major_hint` | A major release is the operator's decision by default. Moving it to auto is a deliberate choice to record with its evidence. | settings hint |

Please send translations of the 13 keys into DE, ES, FR and JA through the usual Compose Multiplatform pipeline (use the app's own word for Automaton: de Automat, es autómata, fr automate, ja オートマトン); the PWA bundles will then mirror them in place of the placeholders.

### Acceptance (mobile)

- The merge pipeline screen shows a Release approval block with one row per size, each showing the mode from `release_approval_in_effect`.
- Choosing a mode on a row saves that project's override through `PUT /api/config` and reloads the screen; a refused `PUT` shows the daemon's message built from `merge_pipeline_save_failed`.
- Settings > Automation has the three global fields; each reads its current value from the daemon config, falls back to the default in the table when absent, and writes back through the config endpoint.
- A release question raised by `ask` mode appears in the existing questions list with its plan text, `yes` and `no` options and `no` as the default, and answering it uses the existing answer path. No second question appears for the same project, Automaton and size.
- Every label, hint and option is localized in EN, DE, ES, FR and JA, with every user-visible string saying Automaton or Automata.
- A missing `release_approval_in_effect` renders as today, so the app stays forward-compatible with older daemons.

## Filing record

Not filed yet. The supervisor checks for an open issue first (`gh issue list -R dmz006/datawatch-app --state all --search 'release approval'`), files this once at the merge with `gh issue create -R dmz006/datawatch-app --title "<the title above>" --body-file <scratch draft>`, and replaces the first line of this file with the issue reference.
