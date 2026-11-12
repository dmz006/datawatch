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

### Billing profile (`usage.billing`)

Landed with the same release (already committed on the branch): a per-provider **billing
profile** on the rate card, so the cost report and the usage calculator can label how an
operator actually pays for a provider instead of guessing from the price fields.

- **`usage.billing.<provider>.*`** — an open map keyed by the provider name from the LLM
  registry (`claude-code`, `openai`, `gemini`, `ollama`, … or any other key without dots). The
  fields: `mode` (`bundled_plan` — a flat monthly subscription whose allowance resets on a
  schedule, `api_metered` — pay-per-use API spend, `free_local` — local inference at no
  charge), `plan` (a free-text label; the claude-code tiers `pro` / `max_5x` / `max_20x`),
  `monthly_price` (finite, ≥ 0, `0` for free plans) and `currency` (ISO 4217), plus an
  optional `reset_weekday` + `reset_time` (the allowance's reset) and `allowance_unit`
  (`percent` default, `requests` or `tokens`). Every field is optional and independent; a
  blank string or `0` clears one back to unset.
- **The default assumption.** A provider with no profile at all is read back as `api_metered`
  with `percent` allowances, and every surface flags it (an "assumed" / "(assumed — no
  usage.billing entry)" label) rather than presenting it as a fact. The assumed default
  carries no price, so the money figures read zero — the daemon would rather show no
  estimate than invent one. Setting a profile removes the assumption.
- **The report:** a `bundled_plan` profile is what makes a cost-report row carry the
  **"bundled plan — API-equivalent, not billed"** label and its point figures
  (`api_equivalent_usd`, `estimated_usage_points`, `share_of_monthly_price`) — the
  API-equivalent estimate, never a charge and never a zero or a fallback price (the
  `rate_configured` / assumed-default labels keep saying what the line is).
- **Release grouping** (`group_by=release`) is exposed only where a shipped-units source is
  configured (the `ShippedUnits` adapter on the server, `internal/server/cost_report.go`):
  the CLI (`--group-by release`), the comm channel and the REST route accept it, and an
  Automaton without a shipped attribution lands under `unreleased`. The read-only MCP
  `cost_report` tool explicitly refuses `group_by=release` with a message naming the accepted
  groupings and the REST route (it exposes no shipped-units source, and it can never be
  wired to one). A request without a shipped-units source is a surfaced error, never empty
  or zero-filled rows.
- **Surfaces:** config block and validation in `internal/config/billing_profile.go` /
  `billing_profile_test.go`; the report engine and its tests in
  `internal/costreport/report.go` / `report_test.go`; the REST keys and handler tests in
  `internal/server/billing_profile_config.go` / `billing_profile_config_test.go`; the PWA
  billing card and its 14 Node tests in
  `internal/server/web/app-usage-billing.test.js`; the CLI billing and release units columns
  in `cmd/datawatch/cli_cost_report_test.go`; the calculator read in
  `internal/server/usage_calculator_advice.go`. Operator guide:
  `docs/howto/usage-calculator.md` ("The billing profile (`usage.billing`)" section, with the
  field reference in `docs/api/cost.md` and the cost-report reading in
  `docs/howto/cost-report.md`). Definitions: `docs/datawatch-definitions.md` (the "Billing
  Profile (`usage.billing`)" entry under "Cost Report"). Companion-parity draft:
  `docs/plans/parity-issues/2026-11-03-billing-profile-parity.md` (the shipped shape is
  final; its "still a draft" line is superseded).

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

## B7 monitoring

To confirm in use: the card reads the report read-only (a group and window change re-queries,
and the retention line and the lifetime note stay from the body), the MCP `cost_report` tool
forwards the window and refuses `group_by=release` with its message, the
`cost_report.` and `usage.billing.` settings fields round-trip through `PUT`/`GET /api/config`
on a live daemon, and the usage calculator reads the point value of the configured provider
profile instead of the assumed-default zero. The REST round-trip and the MCP refusal are the
rows in `docs/testing-tracker.md` ("Cost report" and "Billing profile" sections). Live
validation is left unmarked until the smoke task runs them.

## Parity surface

| Surface | Included? | Intent |
|---|---|---|
| REST | Yes | `GET /api/cost/report` with optional `group_by`, `since`, `until`; `GET` and `PUT /api/config` carry the five `cost_report.*` keys, one bad value refusing the put and naming the key; the route is in `GET /openapi.json`. |
| MCP | Yes | The read-only `cost_report` tool (`group_by`, `since`, `until` all optional) proxies the route; `config_set` takes the `cost_report.*` keys; the tool is listed in the MCP surface listing. |
| CLI | Yes | `datawatch cost report [--group-by automaton\|automation\|backend\|week] [--since <t>] [--until <t>] [--format json]` with the default table (one line per group, tokens first, USD only where a rate covers the group); `datawatch config set cost_report.<key> <value>` covers every key. |
| comm channel | Yes | `cost report [group_by=<g>] [since=<t>] [until=<t>]` and `configure cost_report.<key>=<value>`; the verb is in `cost help`; bare `cost report` sends no parameters. |
| YAML/config | Yes | The `cost_report` block with all five keys and their defaults in `docs/config-reference.yaml`; an unset key reads its default and a hand-edited wrong value stays at its default. |
| PWA | Yes | The Cost report card on the dashboard (rows from the body, tokens first, USD only where the body carried it, retention line and lifetime note from the body), the five Settings → Cost Report fields with help links, and the locale keys in all five bundles. The billing profile card in Settings edits one provider's fields at a time (mode, plan, monthly price and allowance unit), pins the provider in edit mode, reads both config and backends on load and never assumes a disabled backend is on. |
| Config (billing profile) | Yes | `usage.billing.<provider>.<field>` (mode, plan, monthly_price, currency, reset_weekday, reset_time, allowance_unit) on `PUT`/`GET /api/config`; a provider with no entry reads the assumed `api_metered`/`percent` default, flagged as assumed on every surface; a bad mode, a negative or non-finite monthly_price, a bad reset weekday or time and an unknown allowance unit are refused naming the key, with nothing applied (REST and MCP `config_set`). |
| Android | Sync issue, filed | `docs/plans/parity-issues/cost-report.md` (draft — file on datawatch-app once, record the issue URL and number there). |
| iPhone/iOS | Sync issue, filed | Same single parity record: `docs/plans/parity-issues/cost-report.md`. The apps pick the change up from that issue, not from this branch. |
