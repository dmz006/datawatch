# Cost report: the how-to, definitions and parity

**Date:** 2026-11-02
**Version:** none on the branch. This work changes neither `var Version` line in
`cmd/datawatch/main.go` nor `internal/server/api.go`; the merge helper assigns the next
patch above `main` and bumps a major version at merge time. The feature this documents was
landed on the branch in the earlier commits of the same release.

**Terminology.** Operator-facing text says **Automaton / Automata**, following the
Terminology Rule in `AGENT.md`. No tracker IDs appear in any operator-facing file.

## What it is

A **token-first report of model token spend** over a window and a grouping. For each group it
counts the sessions and shows **tokens in / tokens out first**, and a **USD estimate where and
only where configured cost rates cover every backend in the group** — where a rate is missing,
the row shows tokens and `rate_configured` false, and no dollar figure is shown: not a zero,
not a fallback price. Dollar figures are estimates at configured rates, not invoices.

- **Groupings:** `automaton` (default), `automation`, `backend`, `week` (weeks start at
  `cost_report.week_start`, Monday or Sunday).
- **Window:** `since`/`until` in RFC3339 or `YYYY-MM-DD`; an absent `until` is now, an absent
  `since` is `cost_report.default_window_days` (7) back.
- **Retention:** the figures rest on the 31-day ledger of recorded sessions; spend older than
  the ledger is in lifetime totals only (`retention_days`, `lifetime_fallback`).
- **Settings:** under `cost_report.` — `default_group_by` (`automaton`),
  `default_window_days` (7), `show_usd_estimates` (true), `week_start` (`monday`),
  `include_peer_spend` (false).

## Scope

Documentation, definitions and parity work on top of the feature already on the branch:

- `docs/howto/cost-report.md` — the operator how-to with executable steps, the settings
  table, the all-access-methods table and the "what it does not do" boundary.
- `docs/datawatch-definitions.md` — the **Cost Report** glossary entry (anchor `#cost-report`)
  expanded with the four groupings, the window semantics, the retention and
  lifetime-fallback fields and both the peer-spend and USD-visibility settings.
- `docs/operations.md` and `docs/setup.md` — the `cost_report` settings sections with the
  five keys, their defaults and the same access-methods table as the how-to.
- `CHANGELOG.md` — the entry under `[Unreleased]`.
- `docs/testing-tracker.md` — the **Cost report** section with the endpoint contract and
  the exact test names per surface.
- The single mobile parity record, `docs/plans/parity-issues/cost-report.md` (draft, to be
  filed on datawatch-app like the burn guard issue).

The feature itself, already committed:

- `internal/costreport/` — the one report engine: groupings, window, retention and
  lifetime-fallback fields, the USD semantics and the defaults.
- `internal/config/config.go` — the `cost_report` block with the five keys and their
  defaults; a wrong value read by hand stays at its default, a wrong value written through a
  surface is refused naming the key.
- `internal/server/` — `GET /api/cost/report`, the `cost_report.*` keys on `PUT`/`GET
  /api/config`, the OpenAPI entry and the locale keys.
- `internal/mcp/` — the read-only `cost_report` tool and the `cost_report.*` keys of
  `config_set` (the state-changing tool).
- `cmd/datawatch/` — `datawatch cost report` with `--group-by`, `--since`, `--until`,
  `--format json` and the default table.
- `internal/router/` — the comm channel verb `cost report [group_by=…] [since=…] [until=…]`
  and `configure cost_report.<key>=<value>`.
- `internal/server/web/app.js` and `cost-report-view.js` — the Cost report card, the
  Settings → Cost Report group and the five bundles.

## Phases

| Story | What it delivers | Status |
|---|---|---|
| The one report engine and the settings block | `internal/costreport`, the `cost_report` config block with the five keys and defaults, and the engine unit tests | Done |
| The same read and settings on REST, MCP, CLI and the comm channel | `GET /api/cost/report`, the config keys, the `cost_report` MCP tool and the `cost_report.*` keys of `config_set`, `datawatch cost report`, `cost report` and `configure` on the comm channel, each with unit and handler tests | Done |
| The card and the settings in the web app | The Cost report card, the five settings fields with help links, the locale keys in all five bundles, and the Node tests | Done |
| How-to, definitions, parity and the rest of the docs | The how-to, the glossary entry, the operations and setup settings sections, the changelog entry, the testing-tracker section, this plan and the single mobile parity record | Done |

## Status

Done. Every phase above is covered by unit and integration tests on this branch
(`docs/testing-tracker.md`, section "Cost report"). Tested: yes (the engine's 18 tests, the
REST, MCP, CLI and comm channel tests, the PWA Node tests and the locale tests all pass).
Validated: left unmarked until the smoke task exercises the feature on a running daemon; a
unit test alone never marks a row Validated.

## Decision notes

- **Tokens first, dollars second and only where earned.** Where a rate is missing the row
  says so (`rate_configured` false) and shows no figure. A zero would mean "nothing was
  spent" and a fallback price would be made-up billing; the operator keeps the truth.
- **One engine, many surfaces.** The read and the defaults live in one place, so the CLI,
  MCP, comm channel, REST and PWA agree for the same window and grouping and none keeps its
  own copy of the rules.
- **The settings are the one place defaults live.** An unset key reads `automaton`, 7 days,
  USD on, Monday-start weeks and peer spend off, on every surface; the PWA shows the
  settings it reads from the daemon and carries no local copy.
- **The report does not act.** It never spends beyond what was recorded, never ends a
  session, never bills and never guesses a price. It estimates from rates you configure.

## Parity surface

| Surface | Included? | Intent |
|---|---|---|
| REST | Yes | `GET /api/cost/report` with optional `group_by`, `since`, `until`; `GET` and `PUT /api/config` carry the five `cost_report.*` keys, one bad value refusing the put and naming the key; the route is in `GET /openapi.json`. |
| MCP | Yes | The read-only `cost_report` tool (`group_by`, `since`, `until` all optional) proxies the route; `config_set` takes the `cost_report.*` keys; the tool is listed in the MCP surface listing. |
| CLI | Yes | `datawatch cost report [--group-by automaton\|automation\|backend\|week] [--since <t>] [--until <t>] [--format json]` with the default table (one line per group, tokens first, USD only where a rate covers the group); `datawatch config set cost_report.<key> <value>` covers every key. |
| comm channel | Yes | `cost report [group_by=<g>] [since=<t>] [until=<t>]` and `configure cost_report.<key>=<value>`; the verb is in `cost help`; bare `cost report` sends no parameters. |
| YAML/config | Yes | The `cost_report` block with all five keys and their defaults in `docs/config-reference.yaml`; an unset key reads its default and a hand-edited wrong value stays at its default. |
| PWA | Yes | The Cost report card on the dashboard (rows from the body, tokens first, USD only where the body carried it, retention line and lifetime note from the body), the five Settings → Cost Report fields with help links, and the locale keys in all five bundles. |
| Android | Sync issue, filed | `docs/plans/parity-issues/cost-report.md` (draft — file on datawatch-app once, record the issue URL and number there). |
| iPhone/iOS | Sync issue, filed | Same single parity record: `docs/plans/parity-issues/cost-report.md`. The apps pick the change up from that issue, not from this branch. |
