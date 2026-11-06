Issue: https://github.com/dmz006/datawatch-app/issues/380 (datawatch v9.101.0)

# [sync] Post-ship hooks: the autonomous.merge_pipeline.post_ship settings block, the post_ship state in GET /api/autonomous/merge-pipeline, the Post-ship hooks PWA card with its help links, 28 locale keys

## Body of the issue

Post-ship hooks are the small steps a project runs after the ship is done — after merge, version bump, tag, push and deploy have all succeeded. The hook list and the targets its built-ins use are configuration in one global block, `autonomous.merge_pipeline.post_ship`, not code: a project that lists no hooks, or leaves them disabled, ships exactly as it does today. **Hooks are off by default** — the whole block is off, and every hook in the list carries its own switch that is off until turned on; a hook that is listed but disabled does nothing and produces no step in the run. A hook that fails never blocks or fails the ship; its step carries its redacted error and the later hooks still run.

The PWA shows one new card — **Settings > Automation > Post-ship hooks** — and that card is the reference render. The Android and iPhone clients must show the same state and the same switches; mobile config-input is out of scope per the Configuration Accessibility Rule (read-only render, no editing). This records the exact settings block, the REST bodies to read and write, the card's keys and help links, and the locale keys.

### New settings: `autonomous.merge_pipeline.post_ship`

The block is **global**: one list and one set of targets for all projects. Unlike the other `autonomous.merge_pipeline` keys it has no per-project form — `projects.<project>.post_ship.*` is refused by the daemon with a message naming the block, not the value.

| Key | Type | Default | Valid values |
|---|---|---|---|
| `autonomous.merge_pipeline.post_ship.enabled` | boolean | `false` | `true`, `false`; `false` (the default) turns the whole block off, so not one hook runs |
| `autonomous.merge_pipeline.post_ship.hooks` | object array | `[]` (none) | at most 20 hooks, in run order; each is one row of the table below |
| `autonomous.merge_pipeline.post_ship.registry_file` | string | `` (empty) | a path relative to the project; `{project}` is substituted at run time; empty means no target, and an enabled `registry_row` hook with an empty target is refused by name |
| `autonomous.merge_pipeline.post_ship.registry_row_pattern` | string | `` (empty) | the text that must match exactly one row of the registry file; `{project}` and `{version}` are substituted; used by `registry_row` |
| `autonomous.merge_pipeline.post_ship.companion_repository` | string | `` (empty) | `owner/name` of the companion repository the `parity_issue` hook files its issue in (never the project that was merged) |
| `autonomous.merge_pipeline.post_ship.issue_title_template` | string | `` (empty) | the `parity_issue` issue's title; `{project}` and `{version}` are substituted |
| `autonomous.merge_pipeline.post_ship.notify_template` | string | `` (empty) | the `notify` hook's message body; `{project}` and `{version}` are substituted; also the `parity_issue` body when set, without it the issue gets a default one |

Each entry of `hooks[]`:

| Field | Type | Notes |
|---|---|---|
| `name` | string | required and unique; the label shown in the run record |
| `builtin` | string, omitted when empty | `registry_row`, `parity_issue`, `tag` or `notify`; exactly one of `builtin` or `command` must be set |
| `command` | string, omitted when empty | any shell command line; exactly one of `builtin` or `command` must be set |
| `enabled` | boolean | the hook's own switch; `false` by default — a listed hook does nothing until it is on, and a disabled hook produces no step in the run at all |

Nothing turns on by being mentioned: the daemon never guesses a registry file, a companion repository or a message body — an enabled built-in whose target is empty is refused by name at write time.

### REST

#### `GET /api/autonomous/merge-pipeline?project=NAME`

The existing response body gains one field, `post_ship`; it is the same for every project because the block is global:

| Field | Type | Notes |
|---|---|---|
| `post_ship.enabled` | boolean | whether the whole block is on; `false` (the default) means no hook would run |
| `post_ship.hook_count` | integer | how many hooks are defined |
| `post_ship.enabled_hooks` | string array, never `null` | the names of the hooks that would run, in list order; empty when the block is off or has none enabled |

`GET /api/config` serves the full block: `autonomous.merge_pipeline.post_ship` holds `enabled`, `hooks` (each entry as the four fields above) and the five target strings, so a client can render the whole state read-only from a config read.

#### `PUT /api/config`

Body (JSON): the dotted keys above, one or more per request. The daemon checks every key of the block before applying any, so one bad value refuses the whole `PUT` with a message naming the key and the valid values: a hook name is required and must be unique, a hook sets exactly one of `builtin` or `command` (both or neither is refused), an unknown built-in is refused by name, a `hooks` list over 20 is refused, and an enabled built-in whose target is unset in the applied block is refused by the hook's name — although a single `PUT` may set the target and the hook together. `enabled` takes a JSON boolean; the five targets take strings; `hooks` takes the array of the rows above (or that JSON as text, as the comm channel sends it), and a `null` or empty `hooks` clears the list.

### PWA card (reference render)

**Post-ship hooks card** in Settings > Automation, one group and one card:

- The block switch, `post_ship.enabled` (off by default).
- The hook list: one row per hook in run order — the hook's own switch (off by default), its name, the kind (`builtin` or `command`), a select of the four built-ins or a command text box, and up / down / remove buttons; an Add hook button disabled at 20; a **Save hooks** button that sends the whole list through `PUT /api/config` as `autonomous.merge_pipeline.post_ship.hooks`. A refused save shows the daemon's message as the toast; an empty list renders the no-hooks text.
- The five target fields (registry file, registry row pattern, companion repository, issue title template, notify template, the last a textarea), each with its hint and placeholder.
- **Help links**: every card label and field carries a hint link into the sections of the Post-ship hooks how-to (`howto/post-ship-hooks.md`); the first matching prefix wins, so the specific rows come first. `enabled` links to `#enabling`; the hook list and its per-row keys link to `#the-hook-list`; the five targets link to `#the-built-in-targets`; the card title and anything else link to the whole page.

### New locale keys (28)

The `de`, `es`, `fr` and `ja` PWA bundles already carry these keys translated; use the bundle values as the reference translations.

| Key | EN value | Context |
|---|---|---|
| `post_ship_card_title` | Post-ship hooks | card title |
| `settings_post_ship_enabled` | Post-ship hooks (run the enabled hooks once a merge pipeline succeeds; off by default) | settings label |
| `settings_post_ship_enabled_hint` | Off by default. A hook runs only when both this switch and the hook's own switch are on. | settings hint |
| `settings_post_ship_hooks` | Post-ship hook list (the hooks in run order, each with its own switch) | settings label |
| `settings_post_ship_hooks_hint` | The hooks run top to bottom after a successful merge. Every hook is off until you switch it on; the save button sends the whole list. | settings hint |
| `settings_post_ship_registry_file` | Registry file (the file whose row the registry_row hook marks shipped; {project} is substituted at run time) | settings label |
| `settings_post_ship_registry_file_hint` | Relative to the project root. | settings hint |
| `settings_post_ship_registry_row_pattern` | Registry row pattern (the text that must match exactly one row; {project} and {version} are substituted at run time) | settings label |
| `settings_post_ship_registry_row_pattern_hint` | The hook marks the matched row's status cell shipped with the version. | settings hint |
| `settings_post_ship_companion_repository` | Companion repository (repository the parity_issue hook opens the issue in) | settings label |
| `settings_post_ship_companion_repository_hint` | The issue is opened in this companion repository, never in the project that was merged. | settings hint |
| `settings_post_ship_issue_title_template` | Issue title template (the title the parity_issue hook files; {project} and {version} are substituted at run time) | settings label |
| `settings_post_ship_issue_title_template_hint` | {project} becomes the project name and {version} the shipped version. | settings hint |
| `settings_post_ship_notify_template` | Notify template (the message the notify hook sends; {project} and {version} are substituted at run time) | settings label |
| `settings_post_ship_notify_template_hint` | Also the parity_issue hook's body when set; without it the issue gets a default body. | settings hint |
| `post_ship_hook_add` | Add hook | card button |
| `post_ship_hook_empty` | No hooks yet — add one. | card text |
| `post_ship_hook_max` | At most {max} hooks. | card text (max 20) |
| `post_ship_hook_kind_builtin` | builtin | card select option |
| `post_ship_hook_kind_command` | command | card select option |
| `post_ship_hook_name_placeholder` | name | card input placeholder |
| `post_ship_hook_command_placeholder` | any shell command | card input placeholder |
| `post_ship_hook_up` | move up | card button |
| `post_ship_hook_down` | move down | card button |
| `post_ship_hook_remove` | remove | card button |
| `post_ship_save` | Save hooks | card button |
| `post_ship_saved` | Hooks saved | card status |
| `post_ship_save_failed` | Saving the hooks failed | card status (the daemon's message replaces it when one comes back) |

### Acceptance (mobile)

- **The same state, the same switches.** Both clients render the post-ship hook state read-only, matching the card: the block switch (`post_ship.enabled`), the hook list in run order (each hook's name, its own switch, and the built-in or its command), and the five target strings — from `GET /api/autonomous/merge-pipeline` (`post_ship`: enabled, `hook_count`, `enabled_hooks`) and/or `GET /api/config` (`autonomous.merge_pipeline.post_ship`), and the run record where each enabled hook is one step of kind `post_ship_hook` with its result (a failed hook's step carries its redacted error; a skip, like an already-existing tag, reads as a success with the reason).
- **Config-input is out of scope per the Configuration Accessibility Rule.** The card's editing — add / reorder / remove rows, the target fields, Save hooks — stays in the PWA; the mobile clients add no input or edit control for any `post_ship` key, and no `PUT /api/config` writes for them. A refused or out-of-scope write has no place in the mobile render.
- **Hooks off by default.** An unset block reads as off with no hooks and empty targets (`false`, `hook_count` 0, `enabled_hooks` empty, all targets empty); a client pointed at an older daemon that has no `post_ship` block renders the off / empty state, not an error, so the app stays forward-compatible.
- When the endpoints are unavailable (503 or the capability missing) the section is hidden — never a stale state.
- Every label, hint and button is localized in EN, DE, ES, FR and JA, with the PWA bundle values as reference, and every user-visible string says Automaton or Automata.

## Filing record

Not filed yet. The supervisor checks for an open issue first (`gh issue list -R dmz006/datawatch-app --state all --search 'post-ship hooks'`), files this once at the merge with `gh issue create -R dmz006/datawatch-app --title "<the title above>" --body-file <scratch draft>`, and replaces the first line of this file with the issue reference.
