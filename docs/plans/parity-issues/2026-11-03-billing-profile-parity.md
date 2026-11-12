# [sync] Per-provider billing profiles: cost-report billing columns + calculator point value (datawatch next patch release)

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.

Parent release: the datawatch patch release that added per-provider billing
profiles (`usage.billing`) — the cost-report billing columns and the usage
calculator's point-value source.

## What changed in datawatch

Three related surfaces landed together, all driven by one new config block —
per-provider **billing profiles** under `usage.billing.<provider>`, where the
provider key is the name from the daemon's LLM registry (`claude-code`,
`openai`, `gemini`, `ollama`, any name without dots):

- **The cost report** now labels every row for how the provider's spend is
  read. A new `billing_mode` / `billing_label` pair is on the body's rows,
  and where the group's provider is a `bundled_plan` profile the row's money
  moves into `api_equivalent_usd` (an API-equivalent, not a charge), with
  `estimated_usage_points` and `share_of_monthly_price`. A provider with no
  profile reads with the `(assumed — no usage.billing entry)` label.
- **The usage calculator** reads the `claude-code` profile for its point
  value (from `monthly_price`) and, when the profile names
  `reset_weekday`/`reset_time`, for the allowance reset the advice counts
  down to. The point-value source reads back as `"profile"` when set,
  `"default"` when it is the assumed one (price zero) — the zero is the
  daemon refusing to invent an estimate.
- **The config surface** — seven writable fields per provider, optional and
  independent, live the moment stored: `mode`
  (`bundled_plan`|`api_metered`|`free_local`), `plan`, `monthly_price`,
  `currency`, `reset_weekday`, `reset_time`, `allowance_unit`
  (`percent`|`requests`|`tokens`). One invalid value rejects the whole call
  naming the key. The block reads back under `usage.billing` on
  `GET /api/config`.

The app-side operator story for writing and reading all of it is in
[docs/howto/usage-calculator.md](../../howto/usage-calculator.md) (the
**billing profile (usage.billing)** section) and
[docs/howto/cost-report.md](../../howto/cost-report.md) (the **billing
profiles**, **per-release roll-up** and **retention window** sections).

## REST contract changed for the app

- `GET /api/config` — new `usage.billing` object (provider-keyed map of
  profiles; `{}` when none are set).
- `PUT /api/config` — new writable keys `usage.billing.<provider>.<field>`
  (the seven fields; `plan` is free text), one field per request. `400`
  (message names the key) with nothing stored on a bad value or an unknown
  provider/field. No restart: the next cost report and calculator advice
  already read the change.
- `GET /api/cost/report` — row-level additions: `billing_mode`
  (`bundled_plan`|`api_metered`|`free_local`|`mixed`), `billing_label`
  (one of the five fixed label strings, including
  `"api_metered (assumed — no usage.billing entry)"` when the provider has
  no profile), and, for `bundled_plan` rows that are priced,
  `api_equivalent_usd` + `estimated_usage_points` +
  `share_of_monthly_price` (all estimates; `share` is a fraction, 0.05 =
  5%). Body keeps `retention_days` (31), `retention_note` and
  `lifetime_fallback`. `group_by=release` rows carry `units` (shipped units;
  blank when the record has none), and un-shipped work lands under the
  `unreleased` row.
- The MCP `cost_report` tool rejects `group_by=release` by design (it does
  not expose shipped-unit state) — a different surface, not a fallback.

Nothing was removed; every field is additive and absent (not zero) where the
daemon has no figure.

## Settings

| Key | Accepts | Default when unset |
|-----|---------|--------------------|
| `usage.billing.<provider>.mode` | `bundled_plan`, `api_metered`, `free_local` | none — the provider reads as *assumed* `api_metered` |
| `usage.billing.<provider>.plan` | free text (claude-code tiers: `pro`, `max_5x`, `max_20x`) | blank |
| `usage.billing.<provider>.monthly_price` | finite number ≥ 0 | unset (point figures read no value, not zero-as-fact) |
| `usage.billing.<provider>.currency` | 3-letter code | blank |
| `usage.billing.<provider>.reset_weekday` | `sunday`–`saturday` | unset (calculator keeps its own reset) |
| `usage.billing.<provider>.reset_time` | `HH:MM` | unset |
| `usage.billing.<provider>.allowance_unit` | `percent`, `requests`, `tokens` | `percent` |

## Acceptance (app)

- Settings screen: a "billing profiles" group per provider — mode, plan,
  monthly price, currency, reset, unit — with the assumed default marked
  where a provider has no profile. Saving one field stores it and the change
  is live without a restart.
- Cost report view: every row shows tokens first, then — where the provider
  has a profile — the billing label and the mode-specific figures; an
  unsupplied provider's row is visibly flagged "(assumed — no usage.billing
  entry)" rather than silently reading a fact.
- The app must not invent point values where the daemon read none, and must
  not render an `api_metered` row as a bundled-plan row (or vice versa) when
  the provider's profile says otherwise.
- Read surfaces that show `billing_label` or `(assumed)` text render it
  verbatim, unchanged locale-by-locale.

## Filing record

- Filed: — (pending; issue number / URL once opened)
