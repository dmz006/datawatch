# Size-aware judge routing

Date: 2026-10-27. Setting: `autonomous.judge_size_threshold_lines` (default 400, 0 = off). How-to: `docs/howto/judge-policy.md` (section Size-aware routing).

## Problem

On 2026-10-26 an Automaton of about 1,000 changed lines timed out on the local judge. The judge limit (`autonomous.judge_timeout_seconds`, 30 minutes) was spent on a model that could not finish a change that large, and only then did the judge move to the next backend. The judge order in `internal/config/judge_policy.go` and the chooser in `cmd/datawatch/judge_policy_select.go` knew nothing about the size of the change, so a large change started on the same backend as a one-line change.

## Design

- **The threshold.** `autonomous.judge_size_threshold_lines`, a whole number of changed lines, default 400 (`JudgeSizeThresholdDefaultLines` in `internal/config/judge_policy.go`), declared as `JudgeSizeThresholdLines` in `internal/config/config.go` and mirrored in the autonomous config in `internal/autonomous/manager.go`. `0` turns the rule off. A negative value is refused.
- **The counter.** `JudgeDiffLines` in `internal/autonomous/judge_diff_size.go` runs `git diff --numstat` from the merge base of the base branch and the Automaton branch to the branch tip, and adds the added and deleted columns. Lock files (`go.sum`, `package-lock.json`, `yarn.lock`, `pnpm-lock.yaml`, `Cargo.lock` and the other package-manager lock files) are excluded. A binary file counts 0. Only committed history counts. The count is bounded by a 30-second timeout and a failed count is never an error for the judge: the judge keeps the default order and the decision log says the count is unknown. `cmd/datawatch/judge_size_route.go` (`measureJudgeSize`) takes the measurement, and `cmd/datawatch/main.go` calls it once per judge call.
- **The reorder.** `judgeSizeRoute` in `cmd/datawatch/judge_size_route.go` is a pure function. At or below the threshold, or with the threshold at 0, the chain keeps its configured order. Above it, `claude-code` moves to the front when it is in the chain and the rest keep their relative order. Each backend appears once, so the one-attempt-per-backend rule is unchanged. `judgeVerifierChooserSized` in `cmd/datawatch/judge_policy_select.go` applies the order to the list the selector reads and to the candidate pool. In `balance` mode the selector picks by load, so the order only breaks ties. A unit that pins its own guardrail backend is not reordered.
- **The decision log.** Every judge pick's reason starts with the rule text, for example `size rule: 1012 changed lines is above the threshold of 400, claude-code tried first`, or `default order: ...` with the reason the rule did not apply. The Automaton decision log therefore shows which rule routed the judge and the line count. The balance-mode and unknown-count cases add their own sentence.
- **The hold, unchanged.** A judge that times out on every backend still holds the Automaton in `needs_review` with the hold reason `judge_timeout_hold` (`internal/autonomous/judge_hold.go`), after the per-backend `judge_timeout_seconds` limit, the per-timeout warnings and the error alert. The size rule changes the order only, not the limit, the attempts or the hold.

## Files touched

- `internal/config/config.go`, `internal/config/judge_policy.go`: the setting, its default and its validation.
- `internal/autonomous/manager.go`, `internal/autonomous/api.go`: the autonomous mirror of the setting.
- `internal/autonomous/judge_diff_size.go`: the line counter.
- `cmd/datawatch/judge_size_route.go`, `cmd/datawatch/judge_policy_select.go`, `cmd/datawatch/main.go`: the reorder, the chooser wiring and the decision-log text.
- `internal/server/api.go`: REST read and write, and validation of `PUT /api/config`.
- `internal/mcp/autonomous.go`: the `autonomous_config_set` argument.
- `cmd/datawatch/cli_autonomous.go`: the `--judge-size-threshold-lines` flag.
- `internal/router/judge_policy_cmd.go`, `internal/router/router.go`: the comm verbs and help text.
- `internal/server/web/app.js` and `internal/server/web/locales/{en,de,es,fr,ja}.json`: the PWA field and its label and help text.
- Docs: `docs/howto/judge-policy.md`, `docs/config-reference.yaml`, `docs/implementation.md`, `docs/operations.md`, `docs/mcp.md`, `docs/api-mcp-mapping.md`, `docs/api/autonomous.md`, `docs/api/openapi.yaml`, `CHANGELOG.md` and this file.

## Parity surface

Included:

| Surface | File or verb |
|---|---|
| REST | `GET/PUT /api/autonomous/config` and `PUT /api/config` with `autonomous.judge_size_threshold_lines`, in `internal/server/api.go` |
| MCP | `autonomous_config_get` / `autonomous_config_set`, argument `judge_size_threshold_lines`, in `internal/mcp/autonomous.go` |
| CLI | `datawatch autonomous config-set --judge-size-threshold-lines` (the comm channel verb is `judge-policy-set`; the CLI has no such subcommand), in `cmd/datawatch/cli_autonomous.go` |
| Comm channel | `autonomous judge-policy` and `autonomous judge-policy-set judge_size_threshold_lines=n`, in `internal/router/judge_policy_cmd.go` |
| YAML / config | `autonomous.judge_size_threshold_lines` in `config.yaml`, declared in `internal/config/config.go`, listed in `docs/config-reference.yaml` |
| PWA | Settings > Automata > Autonomous Automata planning > Judge size threshold, in `internal/server/web/app.js` with the labels in `internal/server/web/locales/*.json` |

Excluded:

| Surface | Reason |
|---|---|
| Android | A capability-parity target, handled by a datawatch-app issue the operator files after merge. |
| iPhone / iOS | A capability-parity target, handled by the same datawatch-app issue the operator files after merge. |

Key list for the datawatch-app issue:

- Config key: `autonomous.judge_size_threshold_lines` (whole number, default 400, 0 = off, negative refused).
- REST: `GET/PUT /api/autonomous/config` field `judge_size_threshold_lines`; `PUT /api/config` key `autonomous.judge_size_threshold_lines`.
- Locale keys: `settings_judge_size_threshold` and `settings_judge_size_threshold_help` (English text in `internal/server/web/locales/en.json`, with de, es, fr and ja in the same directory).
- Field placement: the autonomous settings card, beside the judge policy and judge time limit fields.
