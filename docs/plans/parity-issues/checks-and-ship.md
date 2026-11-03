# [sync] Automation checks and ship steps

Parent release: v9.68.0.
Filing: filed against dmz006/datawatch-app by the merge step when the release is tagged (v9.68.0); the issue number is recorded here and in docs/parity-status.md at that point.

## Body

The daemon gained automatic checks on Automata and ship steps for finished releases. The PWA shows them read-only; please mirror them in datawatch-app.

### New REST routes

- `GET /api/autonomous/checks` — resolved mode of each check plus recorded findings, newest first. Query: `prd_id`, `project_dir`.
- `GET /api/autonomous/ship` — resolved push, tag and CI-watch settings plus recorded outcomes, newest first. Query: `prd_id`, `project_dir`.

MCP tools `checks_status` and `ship_status`, CLI `datawatch autonomous checks|ship`, and chat `autonomous checks|ship` read the same data.

### Settings keys (`/api/config`)

- `autonomous.checks.pre_start_verifier`, `autonomous.checks.plan_rules`, `autonomous.checks.after_done`, `autonomous.checks.registry_readme` (off, shadow or on; default on)
- `autonomous.checks.block_when_missing` (boolean, default on)
- `autonomous.ship.push`, `autonomous.ship.tag`, `autonomous.ship.ci_watch` (off, shadow or on; default on)
- `autonomous.ship.tag_format` (must contain `{version}`, default `v{version}`), `autonomous.ship.remote` (default `origin`), `autonomous.ship.ci_workflows` (comma separated workflow names)

### Locale keys (all five locales: en, de, es, fr, ja)

`checks_card_title`, `ship_card_title`, `checks_ship_help_link`, `checks_ship_read_only`, `checks_ship_mode_off`, `checks_ship_mode_shadow`, `checks_ship_mode_on`, `checks_ship_yes`, `checks_ship_no`, `checks_ship_no_findings`, `checks_ship_more`, `checks_policy_pre_start_verifier`, `checks_policy_block_when_missing`, `checks_policy_plan_rules`, `checks_policy_after_done`, `checks_policy_registry_readme`, `ship_policy_push`, `ship_policy_tag`, `ship_policy_ci_watch`, `ship_policy_tag_format`, `ship_policy_remote`, `ship_policy_ci_workflows`, `ship_policy_ci_workflows_none`, `checks_failed`, `ship_failed`, `settings_checks_group`, `settings_checks_pre_start_verifier`, `settings_checks_block_when_missing`, `settings_checks_plan_rules`, `settings_checks_after_done`, `settings_checks_registry_readme`, `settings_ship_group`, `settings_ship_push`, `settings_ship_tag`, `settings_ship_ci_watch`, `settings_ship_tag_format`, `settings_ship_remote`, `settings_ship_ci_workflows`

### Web cards

- **Automata checks** card: mode of each check, block-when-missing flag, findings list with a "more not shown" line.
- **Automata ship status** card: push, tag and CI-watch modes, tag format, remote, CI workflows, recorded outcomes.
- Settings groups **Checks** and **Ship steps**.
