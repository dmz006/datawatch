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

The report body also carries two facts about the dollar figures: the
**retention** the figures rest on (the ledger keeps 31 days of recorded
sessions, the field is `retention_days`) and a **lifetime-fallback note**
(`lifetime_fallback`) where some dollar figures rest on rates recorded outside
that window. Read spend older than the ledger only through lifetime totals —
it is not broken out as per-window rows.

## The groupings

| `group_by` | One row per |
|---|---|
| `automaton` (default) | Owning Automaton |
| `automation` | Owning Automation — the automation that ran the work |
| `backend` | Backend family |
| `release` | Release (with `units` — the shipped units — per row; work not yet shipped lands under `unreleased`) |
| `week` | Week, using `cost_report.week_start` (Monday by default, or Sunday) |

## The window

`since` and `until` are optional and accept an RFC3339 timestamp or a bare
`YYYY-MM-DD` date. When `until` is absent it is now; when `since` is absent it
is `until` minus the default window in days (`cost_report.default_window_days`,
7). A too-early `since` does not error or clamp: the report is built over the
window you asked for, while the dollar figures still rest on the 31-day
ledger, which the `retention_days` and `lifetime_fallback` fields explain.

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
| **Web UI** | The **Cost report** card on the dashboard (grouping and window controls, the retention line, at most 50 rows shown); Settings → Cost Report for the five keys |
| **REST API** | `GET /api/cost/report?group_by=<g>&since=<t>&until=<t>`; settings via `PUT /api/config` with `cost_report.<key>` and read back on `GET /api/config` |
| **MCP** | `cost_report` (read-only), `config_set` with `key: cost_report.<key>` |
| **Comm channel** | `cost report [group_by=<g>] [since=<t>] [until=<t>]`; settings with `configure cost_report.<key>=<value>` |

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
