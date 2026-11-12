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
    description: Ask for another grouping and window in one call. group_by is one of automaton, automation, backend, release, week (release is REST/CLI-only; this MCP tool rejects it — it needs shipped-unit state it does not expose); since and until accept RFC3339 or YYYY-MM-DD and are optional. Rows carry the billing label read from each backend's LLM entry billing_mode, then the provider's usage.billing profile (e.g. "bundled plan — API-equivalent, not billed"), plus the point figures when it is a bundled plan; a row that mixes billing units says mixed and carries unit_totals instead of one money figure
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
  - tool: cost_charts
    description: Read the charts as one report, cost over time, tool usage and retries and failure categories, for a grouping and window. group_by is automaton, project, model or automation and window is like 7d or 2w; both are optional. A grouping one view does not know leaves that view on its default and the report names it in notes. Tool usage is a count per group, never a tool name
    args: {group_by: project, window: 7d}
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

Each row says how its money figures should be read, from the **billing mode of
the LLM entries** its backends run on (the `billing_mode` field of each
[LLM registry entry](llm-registry.md#set-the-billing-mode-of-an-llm); `api`,
`allowance` or `local`), falling back to the provider's **billing profile** (the
`usage.billing.*` settings) where the entry has no value:

- `billing_mode` — `bundled_plan` (an `allowance` entry), `api_metered` (an `api`
  entry) or `free_local` (a `local` entry); a row that spans differently-priced
  backends says `mixed`,
- `billing_label` — the one line every UI family shows, e.g.
  **“bundled plan — API-equivalent, not billed”** or
  **“api_metered (assumed — no usage.billing entry)”**,
- and for a bundled plan the three point figures: the **API-equivalent USD**
  (an equivalence, not a charge), the **usage points** it represents, and its
  **share of the monthly price**.

No entry value and no profile, on an `api` row? It keeps its ordinary tokens and
estimate and says so with the “(assumed)” label — nothing is invented, nothing is
billed.
An entry that carries its own `billing_mode` is never “assumed”, even with no
`usage.billing` profile: the row reads “api_metered — billed per use” for an
`api` entry.

### The columns, in order

Every row carries the same columns in the same order; the optional ones are
absent (not zero) when the daemon does not have a figure to show:

| Column | Always there? | How to read it |
|---|---|---|
| `key` | yes | The group: the Automaton name, Automation name, backend family, release, or week — whichever `group_by` you asked for. Work with no owner lands under `unattributed`; work not yet shipped, under `unreleased`. |
| `sessions` | yes | How many recorded sessions landed in the group. |
| `tokens_in` / `tokens_out` | yes | The tokens the group used, shown first on every surface. These are the figures you can always trust in the row. |
| `usd_estimate` | only when priced | A provider estimate at your configured rates — present only when a rate covers **every** backend in the group and `show_usd_estimates` is on. Never a zero and never a fallback price where missing. **Absent on `mixed` rows**: the dollars of `api` backends and the points of `allowance` backends are not summed into one number; read the row's `unit_totals`. |
| `rate_configured` | yes | Whether the row's backends all had a rate; `false` is why a priced-looking row has no dollar figure. |
| `billing_mode` | when billing is read | `bundled_plan`, `api_metered`, `free_local`, or `mixed` (the row spans backends billed in different units). Read from each backend's LLM entry `billing_mode`, then the `usage.billing` profile. |
| `billing_label` | when billing is read | The one line every UI family shows for the row, verbatim — see the label table below. |
| `unit_totals` | when billing is read | One total per billing unit the row spans, in the order `dollars`, `points`, `none` — see [Dollars and points are never added together](#dollars-and-points-are-never-added-together). Each carries `unit`, `billing_mode`, `sessions`, `tokens_in`, `tokens_out`, `active_seconds`, and the figure for its unit (`dollars`, or `points` and `percent`; none for `none`). |
| `api_equivalent_usd` | bundled-plan rows only | The money the group would have cost at API rates — an **equivalence, not a charge**; the row's `usd_estimate` is absent on these rows by design. |
| `estimated_usage_points` | bundled-plan rows, priced only | The usage points that amount represents against the allowance, from the profile's monthly price. An estimate. |
| `share_of_monthly_price` | bundled-plan rows, priced only | The fraction of the plan's monthly price (0.05 is 5%). An estimate. |
| `cache_read` / `cache_creation` | yes | Cache tokens the group used, beside the in/out tokens. |
| `tool_calls` | yes | Tool calls the group's sessions made, counted from the Claude transcript. Zero for backends that do not report them. |
| `first_activity` / `last_activity` / `span_seconds` / `active_seconds` | when the group has activity | When the group worked, the span between, and the active time. An Automaton's row carries all of these, so its tokens, time, sessions and tool calls read together. |
| `window_partial` / `window_note` | when the window cuts the group off | Set when the window is older than the ledger or the group started mid-window. |
| `units` | `release` rows only | The shipped units the release carries, from the daemon's shipped-unit state; blank when the record has none |

The body also carries `totals_by_unit` (the same per-unit totals across every
row) and `retention_days`, `retention_note` and `lifetime_fallback` — see
[the retention window](#the-retention-window) below.

### The row labels

The label on a row is set by the *set of* billing modes the row's backends
resolve to (their LLM entry `billing_mode`, then the profile), not by any single
provider. The five labels, verbatim:

| Label | When |
|---|---|
| **“bundled plan — API-equivalent, not billed”** | Every backend in the row bills as a `bundled_plan` (`allowance` entry or `bundled_plan` profile). The three point figures accompany it wherever the row was priced. |
| **“free local inference — not billed, token usage only”** | Every backend is `free_local` (a `local` entry or `free_local` profile). The row has no money figure at all. |
| **“api_metered — billed per use”** | Every backend is `api_metered` and at least one operator set it — an `api` entry value or a `usage.billing` profile. |
| **“api_metered (assumed — no usage.billing entry)”** | No backend in the row has an entry value or a profile: the row runs on the assumed default, and the label says so. |
| **“mixed billing modes — see the per-unit totals”** | The row's backends span more than one mode. The row carries no single money figure (no `usd_estimate`, no point figures); its `unit_totals` give each unit's figure separately. |

### Dollars and points are never added together

Backends are paid for in different units, so the report never sums across them.
Each backend's LLM entry `billing_mode` picks its unit:

| Entry `billing_mode` | Unit | What the total carries |
|---|---|---|
| `api` | `dollars` | `dollars`: the estimate at your configured rates, for `api` backends only. |
| `allowance` | `points` | `points` (the usage points the API-equivalent spend represents) and `percent` (its share of the plan's monthly price) — both only where the profile has a `monthly_price`. Never a dollar total. |
| `local` | `none` | Tokens, sessions and active time only. No money figure and no points. |

Every group row has `unit_totals` (one entry per unit that row spans) and the
report body has `totals_by_unit` (one entry per unit across all rows). Each entry
also carries its own `sessions`, `tokens_in`, `tokens_out` and `active_seconds`,
so a mixed row still tells you how much of its work ran on each kind of backend.
An entry's `dollars`, `points` and `percent` are omitted, not zero, where a rate
or `monthly_price` is missing.

The CLI prints them under the table as a `totals by unit:` block, one line per
unit — for example `dollars (api_metered): dollars $0.02; sessions 1 …` and
`points (bundled_plan): points 1.2, 0.6% of the allowance; sessions 1 …`.

Example: a row holding one `claude-code` session (an `allowance` entry), one
`aider` session (an `api` entry) and one `ollama` session (a `local` entry) reads
`billing_mode: mixed`, has no `usd_estimate`, and carries three `unit_totals`:
`dollars` (the aider spend alone), `points` (the claude-code spend alone) and
`none` (the ollama tokens and time).

## What api, allowance and local mean

Every LLM registry entry carries a `billing_mode`. It says how that entry is
paid for, and the report keeps each kind of spend in its own unit:

| `billing_mode` | Meaning | Unit in the report | Row reads |
|---|---|---|---|
| `api` | Metered: every call is billed per token. | `dollars`, at your configured rates | `api_metered` |
| `allowance` | A flat plan allowance that resets on a schedule; calls draw on it and nothing is billed per call. | `points` and `percent` of the plan's monthly price | `bundled_plan` |
| `local` | Local inference on your own hardware, no per-call charge. | `none`: tokens, sessions and time only | `free_local` |

### How the value resolves

For each backend in a row the daemon takes the first of these that has a value:

1. **The entry's own `billing_mode`.** It always wins.
2. **The `usage.billing` profile** for the entry's kind: `bundled_plan` reads as
   `allowance`, `api_metered` as `api`, `free_local` as `local`.
3. **The kind default**: `claude-code` is `allowance`; `ollama`, `openwebui` and
   `shell` are `local`; every other kind is `api`.

The kind default always gives a mode, so a backend always resolves. What the
“(assumed — no usage.billing entry)” label marks is how it got there: a row
whose backends all resolve to `api` with no entry value and no profile behind
any of them keeps that label, because the mode is a default and not something
you set. Giving the entry a `billing_mode` or the provider a profile removes it.
Once the migration below has run, every registry entry has its own value, so the
label is left for spend from a backend with no registry entry. The profile still supplies
the plan, the monthly price and the reset the point figures use, so an
`allowance` entry needs a profile with a `monthly_price` before the report can
show points. Setting the value is covered in
[Set the billing mode of an LLM](llm-registry.md#set-the-billing-mode-of-an-llm).

### The migration

When the daemon loads its configuration it writes the resolved value, from
steps 2 and 3 above, onto every LLM entry that has none. Behavior does not
change: a row reads the same before and after, because the written value is the
one the report already resolved. An entry that already has a value is never
touched, and a second load changes nothing. After the migration the entry shows
its mode in the Settings page, `GET /api/llms`, `llm_list` and the YAML, and
changing the profile's `mode` no longer moves that entry.

## The billing profiles

Which mode a row is read against comes from each backend's LLM entry
`billing_mode`. When the entry has none, the per-provider profile under
**`usage.billing`** decides — one entry per provider key, where the key is the
provider name from your LLM registry (`claude-code`, `openai`, `gemini`,
`ollama`, or any other name without dots) — and then the kind default (see
[Set the billing mode of an LLM](llm-registry.md#set-the-billing-mode-of-an-llm)).
**The entry's value wins over the profile's `mode`.** The profile still supplies
everything else the figures need: `monthly_price` (the points and the share of
the price), `plan`, the reset and the allowance unit. A profile says *how you pay*
for that provider, so the report can label the money instead of guessing it:

| `mode` | How that provider's spend is read | A row of it shows |
|---|---|---|
| `bundled_plan` | A flat monthly subscription whose allowance resets on a schedule. | The “bundled plan — API-equivalent, not billed” label; the API-equivalent figure moves from `usd_estimate` to `api_equivalent_usd` (an equivalence, not a charge), plus the usage points it represents and its share of the monthly price. |
| `api_metered` | Pay-per-use spend, no allowance. | The “api_metered — billed per use” label and the ordinary `usd_estimate` where rates cover the row. |
| `free_local` | Local inference at no charge. | The “free local inference — not billed, token usage only” label and **no money figure at all** — tokens are the whole answer. |

**The default assumption.** A provider with *no* entry value and *no* profile is read
from the kind default (`claude-code` as a bundled plan, `ollama`, `openwebui` and
`shell` as free local, every other kind as `api_metered`) with percent allowances.
When that default is `api_metered` the row carries the “(assumed — no usage.billing
entry)” label: an assumption, not a fact about the provider. The assumed default
carries no price and no allowance: the row keeps its token and rate-based figures
and nothing more is invented, so a bundled plan with no `monthly_price` shows no
points. Setting an entry value or a profile removes the assumption.

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

## The start-up tables

Alongside the grouped rows, the report carries two start-up tables over the same
window: **by profile** (`startup_by_profile`) and **by role** (`startup_by_role`).
Each row counts the worker attempts that ran with that profile or role, and gives
the average **startup tokens** — the size of the starting context the worker was
launched with. Read them to see which worker profile or role starts with the most
context.

- Each attempt's size is an **estimate** taken at launch from the generated
  starting context: one token per four bytes, rounded up. The row shows how many
  of its attempts were estimated and how many reported a size at runtime.
- Attempts with no size (records from before start-up sizes were captured) are
  counted as **no data**, never as zero, and do not pull the average down.
- Attempts that ran with no profile are grouped under the unprofiled row; attempts
  with no role under the no-role row.
- The tables are advice-free. They do not change any profile or setting.

The same averages appear as the `worker_startup` block of the stats snapshot
(`datawatch stats`, `GET /api/stats`, the `get_stats` MCP tool), and the usage
calculator's start-up advice line is computed from the by-profile table — see
[usage-calculator.md](usage-calculator.md#the-start-up-advice-line).

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

## The chart cards on the Automata page

The Automata page shows the combined usage report as four cards, in the cost
report area of its **Progress and cost** tab. A row of controls above them
sets the grouping (Project, Automation or Automaton), the window (7 days to 8
weeks) and whether the first chart shows tokens or money. Every card has a
one-line purpose, a state, a reason and a next action, and a **?** that opens
this page.

| Card | What it draws | Notes |
|---|---|---|
| **Cost over time** | A line per group: tokens, or money | Money is one chart per billing unit. Each axis is labelled with its unit and billing mode, for example dollars on an API-metered backend and points on a bundled plan, so units are never drawn together or added. When no rate or monthly price is set, or USD estimates are off, tokens are drawn and the card says so |
| **Tool usage** | A line per group: tool calls | Counts only, never tool names |
| **Retries and failure categories** | Failed attempts per week, stacked by category, and a table of attempts, tasks and retries per group | Retries count each task once and restart when a task is reset |
| **Combined usage report** | One table joining tokens, money, tool calls, failed attempts and retries per group | Money is listed per unit on its own line |

Each chart draws at most six lines and sixty points per line, and each table
at most eight rows; the card says when more groups exist. Cost over time and
tool usage are not recorded per Automation, so under that grouping only the
failures chart and the combined table are drawn.

A card whose data is empty, or whose view the daemon cannot supply, is not
drawn. Its reason and next action appear as an entry at the top of the **Events**
tab instead, so a missing chart is always explained.

## All access methods

| Method | How |
|---|---|
| **YAML config** | `cost_report.*` in `~/.datawatch/config.yaml`; see `docs/config-reference.yaml` |
| **CLI** | `datawatch cost report [--group-by automaton\|automation\|backend\|release\|week] [--since <t>] [--until <t>] [--format json]`, a table by default (with a `totals by unit` block under it), JSON the raw body |
| **Web UI** | The **Cost report** card on the dashboard (grouping and window controls, the retention line, at most 50 rows shown); the four chart cards on the Automata page (see above); the **Overview** of an Automaton's detail view shows its cost; Settings → Cost Report for the five keys |
| **REST API** | `GET /api/cost/report?group_by=<g>&since=<t>&until=<t>`; settings via `PUT /api/config` with `cost_report.<key>` and read back on `GET /api/config` |
| **MCP** | `cost_report` (read-only), `config_set` with `key: cost_report.<key>` |
| **Comm channel** | `cost report [group_by=<g>] [since=<t>] [until=<t>]`; settings with `configure cost_report.<key>=<value>` |
| **Combined usage report** | Cost over time, tool usage, and retries and failures in one reading: `datawatch cost charts [--group-by <g>] [--window <d>] [--json]`, the comm channel `cost charts [group_by=<g>] [window=<d>]`, and the read-only MCP `cost_charts` tool. Each reads `GET /api/cost/series`, `GET /api/cost/tool-usage` and `GET /api/cost/failures` and joins them; see `docs/api/cost.md` |

The merged change record (merge report) of an Automaton also carries a cost section, read from the same ledger through the same engine. It lists sessions, tokens in and out, cache read and creation tokens, tool calls, the span, the session ids, and a dollar figure only where a rate is configured (labelled as an API-equivalent on a bundled plan). It adds a window note when the figures cover only part of the Automaton's life, and an unknown-spend note when some spend could not be read. The section is left out when no spend ledger is wired.

The **Overview** of an Automaton's detail view in the web UI shows the same row as `cost` on `GET /api/autonomous/prds/{id}`: sessions, tokens, tool calls and time together, with the same bundled-plan label.

All five read methods answer from the same engine call over the daemon's
recorded sessions, so the same window and grouping give the same rows.

## Billing mode and the usage charts on every surface

The billing mode field and the combined usage report are reachable the same way
on every family of surface. The table lists what each one touched; the mobile
apps are tracked in the parity issue in the `datawatch-app` repository.

| Surface | Billing mode | Usage charts |
|---|---|---|
| **REST** | `billing_mode` on `POST /api/llms`, `PUT /api/llms/{name}` and `PUT /api/config` (`llms.<name>.billing_mode`); returned by `GET /api/llms`, `GET /api/llms/{name}` and `GET /api/config`; row fields `billing_mode`, `billing_label`, `unit_totals` and `totals_by_unit` on `GET /api/cost/report` | `GET /api/cost/series`, `GET /api/cost/tool-usage` and `GET /api/cost/failures` |
| **MCP** | `billing_mode` on `llm_add` and `llm_update`; read back with `llm_get` and `llm_list`; the same row fields on `cost_report` | `cost_charts` (read-only) |
| **CLI** | `datawatch llm add\|update <name> --billing-mode <mode>`; `datawatch config set llms.<name>.billing_mode <mode>`; the `totals by unit` block of `datawatch cost report` | `datawatch cost charts [--group-by <g>] [--window <d>] [--json]` |
| **Comm channel** | `llm add\|update <name> billing_mode=<mode>`; `configure llm.<name>.billing_mode=<mode>` | `cost charts [group_by=<g>] [window=<d>]` |
| **YAML / config** | `billing_mode:` on each entry under `llms`; the fallback profile under `usage.billing.<provider>` | No setting of its own: the default window comes from `cost_report.default_window_days` |
| **PWA** | **Billing mode** on the Settings → Compute → LLM edit form, with help text, in all five locales | The four chart cards in the **Progress and cost** tab of the Automata page; their reasons surface at the top of the **Events** tab |
| **Android, iPhone** | Not changed here; tracked in the parity issue | Not changed here; tracked in the parity issue |

The chart routes take `group_by` and `window`; the series and tool-usage routes
also take `bucket` (`hour` or `day`). Cost over time and tool usage group by
`automaton`, `project` or `model`; retries and failure categories group by
`project`, `automation` or `automaton`. The full contracts are in
[`docs/api/cost.md`](../api/cost.md) and the OpenAPI file.

## What it does not do

- It does not stop, slow or rerun anything. It is advice-free by design: the
  figures are yours to act on.
- It does not bill. The USD figures are estimates at configured rates, and a
  bundled-plan row’s money figures are **API-equivalents — not bills**. Where
  no rate is configured there is no figure, not a zero.
- It does not add dollars to points. A row that mixes billing units shows each
  unit's total separately instead of one blended number.
- It does not retain more than the ledger does: 31 days of recorded sessions,
  with older spend visible only through lifetime totals. The charts read the
  same ledger, so a window that reaches past it is marked unknown, not zero.
- The charts do not name tools. Only a count of tool calls per group is
  recorded, so tool usage cannot be split by tool.
- The retries figure is a floor: each task's retry count is counted once and
  restarts when the task is reset.

Definitions: [`docs/datawatch-definitions.md#cost-report`](../datawatch-definitions.md#cost-report)
and [`#usage-charts-and-the-combined-usage-report`](../datawatch-definitions.md#usage-charts-and-the-combined-usage-report).
