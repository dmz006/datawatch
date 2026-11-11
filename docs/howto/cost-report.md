---
docs:
  index: true
  topics: [cost, billing, usage, reports, tokens, usd]
exec_steps:
  - tool: cost_report
    description: Read the report first with no arguments. It applies the daemon's cost_report settings: the default grouping (automaton by default) and the default window (the last 7 days by default). Tokens are always shown; a group's USD figure appears only where a rate is configured
    args: {}
    read_only: true
  - tool: cost_report
    description: Ask for another grouping and window in one call. group_by is one of automaton, automation, backend, release, week (release is REST/CLI-only; this MCP tool rejects it — it needs shipped-unit state it does not expose); since and until accept RFC3339 or YYYY-MM-DD and are optional. Rows carry the billing label from the owning provider's usage.billing profile (e.g. "bundled plan — API-equivalent, not billed") plus the point figures when it is a bundled plan
    args: {group_by: backend, since: "2026-10-26", until: "2026-11-02"}
    read_only: true
  - tool: config_set
    description: Worked example, a state-changing call. Widen the default window the report falls back to, from 7 days to 14. An unset or missing since in a later report request then spans the last 14 days
    args: {key: cost_report.default_window_days, value: 14}
  - tool: config_set
    description: Worked example, a state-changing call. Turn the USD estimates off entirely. Every surface then shows tokens only, even where rates are configured
    args: {key: cost_report.show_usd_estimates, value: false}
  - tool: cost_report
    description: Read the report again after the setting changes, to see it with tokens only and over the wider window
    args: {}
    read_only: true
---

# Cost report

The cost report is a **token-first report over your recorded sessions**: for a
window and a grouping you choose, it counts the sessions and the tokens they
used, and gives a USD figure **where — and only where — it can**.

It is a report, not a bill, and it acts on nothing. It never stops, reruns,
routes or budgets any work; it only reads the recorded sessions and the rates
you configured, and shows the result.

## What a row is

Each row is one group (what it is grouped by depends on the `group_by` below),
with:

- the number of **sessions** that landed in the group,
- **tokens in and tokens out** — always present, always shown first,
- a **USD estimate** — present only when a rate is configured for every backend
  in that group. Where a rate is missing, the row shows the tokens and no
  dollar figure, and `rate_configured` is `false`.

Where the owning provider has a **billing profile** (the `usage.billing.*`
settings), the row says how its money figures should be read:

- `billing_mode` — `bundled_plan`, `api_metered` or `free_local` (a row that
  spans differently-priced providers says `mixed`),
- `billing_label` — the one line every UI family shows, e.g.
  **“bundled plan — API-equivalent, not billed”** or
  **“api_metered (assumed — no usage.billing entry)”**,
- and for a bundled plan the three point figures: the **API-equivalent USD**
  (an equivalence, not a charge), the **usage points** it represents, and its
  **share of the monthly price**.

No profile? The row keeps its ordinary tokens and estimate and says so with
the “(assumed)” label — nothing is invented, nothing is billed.

### The columns, in order

Every row carries the same columns in the same order; the optional ones are
absent (not zero) when the daemon does not have a figure to show:

| Column | Always there? | How to read it |
|---|---|---|
| `key` | yes | The group: the Automaton name, Automation name, backend family, release, or week — whichever `group_by` you asked for. Work with no owner lands under `unattributed`; work not yet shipped, under `unreleased`. |
| `sessions` | yes | How many recorded sessions landed in the group. |
| `tokens_in` / `tokens_out` | yes | The tokens the group used, shown first on every surface. These are the figures you can always trust in the row. |
| `usd_estimate` | only when priced | A provider estimate at your configured rates — present only when a rate covers **every** backend in the group and `show_usd_estimates` is on. Never a zero and never a fallback price where missing. |
| `rate_configured` | yes | Whether the row's backends all had a rate; `false` is why a priced-looking row has no dollar figure. |
| `billing_mode` | when profiles are read | `bundled_plan`, `api_metered`, `free_local`, or `mixed` (the row spans differently-priced providers). |
| `billing_label` | when profiles are read | The one line every UI family shows for the row, verbatim — see the label table below. |
| `api_equivalent_usd` | bundled-plan rows only | The money the group would have cost at API rates — an **equivalence, not a charge**; the row's `usd_estimate` is absent on these rows by design. |
| `estimated_usage_points` | bundled-plan rows, priced only | The usage points that amount represents against the allowance, from the profile's monthly price. An estimate. |
| `share_of_monthly_price` | bundled-plan rows, priced only | The fraction of the plan's monthly price (0.05 is 5%). An estimate. |
| `cache_read` / `cache_creation` | yes | Cache tokens the group used, beside the in/out tokens. |
| `tool_calls` | yes | Tool calls the group's sessions made, counted from the Claude transcript. Zero for backends that do not report them. |
| `first_activity` / `last_activity` / `span_seconds` / `active_seconds` | when the group has activity | When the group worked, the span between, and the active time. An Automaton's row carries all of these, so its tokens, time, sessions and tool calls read together. |
| `window_partial` / `window_note` | when the window cuts the group off | Set when the window is older than the ledger or the group started mid-window. |
| `units` | `release` rows only | The shipped units the release carries, from the daemon's shipped-unit state; blank when the record has none |

The body also carries `retention_days`, `retention_note` and
`lifetime_fallback` — see [the retention window](#the-retention-window)
below.

### The row labels

The label on a row is set by the *set of* profiles the row's backends have,
not by any single provider. The five labels, verbatim:

| Label | When |
|---|---|
| **“bundled plan — API-equivalent, not billed”** | Every backend in the row is a `bundled_plan` profile. The three point figures accompany it wherever the row was priced. |
| **“free local inference — not billed, token usage only”** | Every backend is `free_local`. The row has no money figure at all. |
| **“api_metered — billed per use”** | Every backend is `api_metered` and at least one operator set that profile. |
| **“api_metered (assumed — no usage.billing entry)”** | No backend in the row has any profile: the row runs on the assumed default, and the label says so. |
| **“mixed billing modes — only the usd_estimate is unambiguous”** | The row's backends span more than one mode; the mode-specific point figures are not meaningful for it. |

## The billing profiles

The labels above come from the per-provider profiles under
**`usage.billing`** — one entry per provider key, where the key is the
provider name from your LLM registry (`claude-code`, `openai`, `gemini`,
`ollama`, or any other name without dots). A profile says *how you pay* for
that provider, so the report can label the money instead of guessing it:

| `mode` | How that provider's spend is read | A row of it shows |
|---|---|---|
| `bundled_plan` | A flat monthly subscription whose allowance resets on a schedule. | The “bundled plan — API-equivalent, not billed” label; the API-equivalent figure moves from `usd_estimate` to `api_equivalent_usd` (an equivalence, not a charge), plus the usage points it represents and its share of the monthly price. |
| `api_metered` | Pay-per-use spend, no allowance. | The “api_metered — billed per use” label and the ordinary `usd_estimate` where rates cover the row. |
| `free_local` | Local inference at no charge. | The “free local inference — not billed, token usage only” label and **no money figure at all** — tokens are the whole answer. |

**The default assumption.** A provider with *no* profile is read as
`api_metered` with percent allowances — an assumption flagged on the row with
the “(assumed — no usage.billing entry)” label, not a fact about the
provider. The assumed default carries no price and no allowance: the row keeps
its token and rate-based figures and nothing more is invented. Setting a
profile removes the assumption.

Setting one is a config write, one field at a time — the same fields the
usage calculator reads for its point value — settable all six ways the
[billing profile section of the usage calculator howto](usage-calculator.md#the-billing-profile-usagebilling)
lists; the short version:

```yaml
usage:
  billing:
    claude-code:
      mode: bundled_plan      # bundled_plan | api_metered | free_local
      plan: max_5x            # free text; claude-code tiers: pro, max_5x, max_20x
      monthly_price: 200      # finite number >= 0, in currency; 0 for a free plan
      currency: usd
      reset_weekday: thursday # optional; where the allowance resets
      reset_time: "00:00"
      allowance_unit: percent # percent (default) | requests | tokens
```

or `datawatch config set usage.billing.claude-code.mode bundled_plan`,
`PUT /api/config` with `"usage.billing.claude-code.mode": "bundled_plan"`,
`configure usage.billing.claude-code.mode=bundled_plan` on the comm channel,
the billing profiles group on the Usage calculator card in PWA settings, or
`config_set` on MCP. Every field is optional and independent; a blank
string (or `0`) clears one back to unset, and a removal puts the provider
back on the assumed default. A change is live immediately — the next report
already reads it — and one invalid value is refused naming the key.

The two read surfaces agree by construction: the calculator's point value
comes from the same `monthly_price`, and the report's `api_equivalent_usd`,
`estimated_usage_points` and `share_of_monthly_price` are the same profile's
money as rows.

### What an Automaton's row counts

An Automaton's row counts every session it launches, not just its worker sessions: worker, verification, rules-review, decompose and guardrail sessions, including each retry session as its own entry. Each session records the Automaton and task it belongs to when it is launched, and the daemon keeps that in a spend ledger (`spend_ledger.json` in the data directory) keyed by session. The row's `sessions` and token columns therefore include planning, verification and retry spend, and the ledger also holds each session's active time and tool-call count (read from the Claude transcript).

- **Deleting sessions does not lower the cost.** The ledger entry outlives the session, so deleting or cleaning up a session leaves the Automaton's cost unchanged until the entry ages out of the 31-day window.
- **Older sessions stay uncounted.** Sessions that ran before this change carry no attribution and land under `unattributed`. Nothing is back-filled; new sessions count from the first launch after the upgrade.

The same per-Automaton row (the whole ledger, not the window) appears as `cost` in `GET /api/autonomous/prds/{id}` (MCP `autonomous_prd_get`), so one Automaton's spend reads without running the report. An Automaton with no recorded spend gets a zero row; `cost` is absent only when no spend ledger is wired. The CLI and comm-channel cost report show the sessions, tool-call and time columns for Automaton rows, and the MCP `cost_report` tool returns them in every row.

### Checking for unattributed sessions

The checks status (`GET /api/autonomous/checks`, MCP `checks_status`) carries an `attribution` object: `automaton_launched` (ledger sessions started by Automaton machinery, meaning a task id or a worker, decompose, rules-review, verification or guardrail role), `unattributed` (those with no Automaton id), `session_ids`, `ok` and a `note`. `ok` is true when none are missing. A non-zero `unattributed` means a launcher skipped the stamp and that spend shows under `unattributed` in the Automaton grouping. The check reads the whole ledger and is absent when no ledger is wired.

### What is recorded per Automaton and role

Each ledger entry records, for one session: the Automaton and task it belongs to, its **role** (`worker`, `decompose`, `rules_review`, `verification` or `guardrail`; a session launched with no role is recorded as `other`), the backend, tokens in and out, cache tokens, tool calls, and the start, end and active time. The Automaton's row adds these up, so decompose, verification, rules-review and retry sessions are part of its cost and not hidden under the worker.

### Reading the window: a missing row, partial rows and unknown spend

- **A missing row is not zero spend.** The default window is 7 days (`cost_report.default_window_days`). An Automaton that does not appear had no activity in the window. Widen `since` to find it.
- **Partial rows.** A row whose first activity is before the window start sets `window_partial` and a `window_note`: its figures cover the window only, not the Automaton's whole life. The report body carries a top-level `window_note` when any row is partial.
- **Unknown spend older than the ledger.** When the window reaches before the 31-day horizon, the report carries `unknown_spend_note` and `unknown_before` (the horizon date), and the affected rows set `spend_unknown` with a `spend_unknown_note`. Spend before that date is **unknown**: it is not zero and is never backfilled.

### Reading the cache tokens

`cache_read` and `cache_creation` sit beside `tokens_in` and `tokens_out` and are counted separately from them. Cache creation is the prompt text written to the provider's cache, and cache read is the cached text reused on a later turn. A large `cache_read` beside a small `tokens_in` is a healthy, cache-heavy session. They are shown as tokens and are not priced separately on the row.

### The API-equivalent label on a bundled plan

On a bundled plan the dollar figure is `api_equivalent_usd`, labeled **“bundled plan — API-equivalent, not billed”**. It is what the same tokens would cost at API rates, shown so Automata can be compared with each other. It is not a charge: the plan is a flat monthly price, and `share_of_monthly_price` and `estimated_usage_points` show how much of the allowance the spend represents.

## The groupings

| `group_by` | One row per |
|---|---|
| `automaton` (default) | Owning Automaton |
| `automation` | Owning Automation — the automation that ran the work |
| `backend` | Backend family |
| `release` | Release (with `units` — the shipped units — per row; work not yet shipped lands under `unreleased`) |
| `week` | Week, using `cost_report.week_start` (Monday by default, or Sunday) |

### The per-release roll-up

`group_by release` is the roll-up for “what did each release cost”. The
differences you should know before reading it:

- **Rows are releases, not work.** Work is attributed to **the release it
  shipped under**, so a release row sums its shipped units' sessions and
tokens. Work whose owner has *not shipped yet* lands under the **`unreleased`**
  row instead of being dropped, and a group that spans different releases
carries the `mixed` key with `units` blank.
- **`units` is on the row.** Each `release` row carries `units`, the count of
  shipped units (PRDs / Automations) that release contains, from the daemon's
  shipped-unit state — that is the roll-up: sessions and tokens *per released
  unit* are yours to divide out, and the `unreleased` row tells you how far
  behind the ships the work is.
- **Availability.** `release` needs the shipped-unit state, so it is served
  by **REST and CLI** (`GET /api/cost/report?group_by=release`,
  `datawatch cost report --group-by release`). The MCP `cost_report` tool
  **rejects** `release` — it is a different surface that does not expose the
  shipped-unit state, and it says so rather than answering a row it cannot
  stand behind. The PWA card's Group by control offers `automaton`,
  `automation`, `backend`, `week` and `default`; `release` is a
terminal/REST grouping.
- **The billing label still applies.** A release row whose backends are all
  a bundled plan reads with the bundled label and its point figures, exactly
  like any other row whose backends say the same.

Example: `rows: [{"key": "v1.4.2", "sessions": 9, "tokens_in": 412000,
"tokens_out": 98000, "units": 3, "billing_label": "bundled plan — API-equivalent,
not billed"}, {"key": "unreleased", "sessions": 4}]` — three shipped units
landed in 1.4.2, four sessions' worth of work is still ahead of any ship
(the `unreleased` row has no `units` key at all), and the bundle reads
“API-equivalent, not billed” because the profiles say so.

## The window

`since` and `until` are optional and accept an RFC3339 timestamp or a bare
`YYYY-MM-DD` date. When `until` is absent it is now; when `since` is absent it
is `until` minus the default window in days (`cost_report.default_window_days`,
7). A too-early `since` does not error or clamp: the report is built over the
window you asked for, while the dollar figures still rest on the 31-day
ledger, which the `retention_days` and `lifetime_fallback` fields explain.

## The retention window

The dollar figures rest on a **31-day ledger of recorded sessions** — that is
the horizon a figure in the report can look back to. The body makes this
explicit on every call:

- `retention_days` — 31. The ledger the figures rest on.
- `retention_note` — a human sentence for the same fact, always present, that
  the UIs surface verbatim (style: *"Dollar amounts rest on the spend
  ledger's 31-day retention window. Spend older than the window is not in
  the ledger."*). Use it as the card's footnote; do not restate the number
  elsewhere in the card.
- `lifetime_fallback` — `true` when your window reaches *further back* than
  the ledger. Then the rows carry the sessions' **lifetime totals** instead of
  in-window spend: per-event spend lines older than the window are pruned,
  and the note says so (the `retention_note` changes to name the fallback).

Reading rules that follow from it:

- **Inside the ledger** (`since` inside 31 days): rows are window-scoped
  tokens, and the dollar figures price that window.
- **Beyond the ledger**: the report still builds over your window, but the
  dollar figures are lifetime totals — compare them to each other, never
  against a window-sized figure. A row for spend older than the ledger is a
  lifetime view, not a broken window.
- **Tokens are always in-window** even when the money is not; the column can
  disagree with the dollars, and that is the retention, not an error.

## Where the dollars come from

- The dollar figures are **estimates at the rates you configure** per backend.
  They are provider estimates, not invoices.
- A row carries a USD estimate **only when a rate covers every backend in it**;
  otherwise the field is absent and the tokens are the answer.
- `cost_report.show_usd_estimates: false` suppresses the figures everywhere, so
  the report is tokens-only even where rates exist.
- `cost_report.include_peer_spend` (default `false`) admits spend your
  federation peers reported to this daemon. Off by default: a report about your
  daemon shows your daemon.

## The settings

| Key | Default | Meaning |
|---|---|---|
| `cost_report.default_group_by` | `automaton` | Grouping a request that names none is built under |
| `cost_report.default_window_days` | `7` | Window in days the report falls back to when a request has no `since` |
| `cost_report.show_usd_estimates` | `true` | Whether rows may carry a USD estimate at all, on top of a rate being configured |
| `cost_report.week_start` | `monday` | The day that starts a week for `week` reports (`monday` or `sunday`) |
| `cost_report.include_peer_spend` | `false` | Whether rows include spend federation peers reported |

Changes apply without a restart. A bad value sent through a write surface is
refused naming the key; a bad value written by hand into the YAML reads the
default.

## All access methods

| Method | How |
|---|---|
| **YAML config** | `cost_report.*` in `~/.datawatch/config.yaml`; see `docs/config-reference.yaml` |
| **CLI** | `datawatch cost report [--group-by automaton\|automation\|backend\|release\|week] [--since <t>] [--until <t>] [--format json]`, a table by default, JSON the raw body |
| **Web UI** | The **Cost report** card on the dashboard (grouping and window controls, the retention line, at most 50 rows shown); the **Overview** of an Automaton's detail view shows its cost; Settings → Cost Report for the five keys |
| **REST API** | `GET /api/cost/report?group_by=<g>&since=<t>&until=<t>`; settings via `PUT /api/config` with `cost_report.<key>` and read back on `GET /api/config` |
| **MCP** | `cost_report` (read-only), `config_set` with `key: cost_report.<key>` |
| **Comm channel** | `cost report [group_by=<g>] [since=<t>] [until=<t>]`; settings with `configure cost_report.<key>=<value>` |

The merged change record (merge report) of an Automaton also carries a cost section, read from the same ledger through the same engine. It lists sessions, tokens in and out, cache read and creation tokens, tool calls, the span, the session ids, and a dollar figure only where a rate is configured (labelled as an API-equivalent on a bundled plan). It adds a window note when the figures cover only part of the Automaton's life, and an unknown-spend note when some spend could not be read. The section is left out when no spend ledger is wired.

The **Overview** of an Automaton's detail view in the web UI shows the same row as `cost` on `GET /api/autonomous/prds/{id}`: sessions, tokens, tool calls and time together, with the same bundled-plan label.

All five read methods answer from the same engine call over the daemon's
recorded sessions, so the same window and grouping give the same rows.

## What it does not do

- It does not stop, slow or rerun anything. It is advice-free by design: the
  figures are yours to act on.
- It does not bill. The USD figures are estimates at configured rates, and a
  bundled-plan row’s money figures are **API-equivalents — not bills**. Where
  no rate is configured there is no figure, not a zero.
- It does not retain more than the ledger does: 31 days of recorded sessions,
  with older spend visible only through lifetime totals.

Definitions: [`docs/datawatch-definitions.md#cost-report`](../datawatch-definitions.md#cost-report).
