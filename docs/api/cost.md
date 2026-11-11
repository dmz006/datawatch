# Cost tracking API (v3.7.0 + v3.7.1)

**Shipped in v3.7.0 (BL6).** Per-session token + dollar accounting
with operator-overridable rate table. v3.7.1 wires the rate table
through full config parity (no hard-coded rates that can't be
overridden).

---

## Surfaces

```
GET  /api/cost                          # rollup across all sessions
GET  /api/cost?session=<full_id>        # per-session breakdown
POST /api/cost/usage                    # add usage to a session
                                          body: {session, tokens_in, tokens_out,
                                                 in_per_k?, out_per_k?}

GET  /api/cost/rates                    # current effective rate table
PUT  /api/cost/rates                    # body: {rates: {backend: {in_per_k, out_per_k}}}
```

`Session` JSON gained:
```json
{ "tokens_in": 0, "tokens_out": 0, "est_cost_usd": 0 }
```

---

## Configuration

Per the no-hard-coded-config rule, every backend rate is overridable
in YAML and via the REST surface above:

```yaml
session:
  cost_rates:
    claude-code:
      in_per_k:  0.003
      out_per_k: 0.015
    gemini:
      in_per_k:  0.0035
      out_per_k: 0.0105
    "my-custom-backend":
      in_per_k:  0.001
      out_per_k: 0.005
```

Empty / missing entries fall through to the built-in defaults
(`session.DefaultCostRates()` in code) so operators only need to
specify what they're overriding. The built-in defaults reflect the
public price card at the time of writing — verify against your plan
and override when they differ.

`PUT /api/cost/rates` writes the override back to the config file
so subsequent restarts persist the change.

---

## Examples

### Add usage from a backend hook

```bash
curl -X POST http://localhost:8080/api/cost/usage \
  -H 'Content-Type: application/json' \
  -d '{"session":"host-aa","tokens_in":1024,"tokens_out":512}'
```

### Query rollup

```bash
curl -sS http://localhost:8080/api/cost | jq .
```

```json
{
  "sessions": 14,
  "total_tokens_in":  120000,
  "total_tokens_out":  45000,
  "total_usd": 1.0275,
  "per_backend": {
    "claude-code": {"sessions": 12, "tokens_in": 110000, "tokens_out": 42000, "usd": 0.96},
    "ollama":      {"sessions":  2, "tokens_in":  10000, "tokens_out":  3000, "usd": 0}
  }
}
```

### Override a rate at runtime

```bash
curl -X PUT http://localhost:8080/api/cost/rates \
  -H 'Content-Type: application/json' \
  -d '{"rates":{"claude-code":{"in_per_k":0.0025,"out_per_k":0.012}}}'
```

The override is applied to the live `Manager` immediately and
persisted to the config file.

---

## AI / MCP integration notes

- `tokens_in/out` and `est_cost_usd` are written by `Manager.AddUsage`,
  which is called by per-backend response parsers (claude-code is wired
  initially; other backends' parsers land in subsequent releases).
  Until then, AI agents can call `POST /api/cost/usage` directly to
  record usage from their own counters.
- All rates are USD per 1K tokens (matching the standard convention
  used by Anthropic, OpenAI, Google).
- Local backends (`ollama`, `openwebui`, `shell`) ship with rate
  `{0, 0}` so their cost stays at $0 by default — operators with
  paid hosting can override.

---

<!-- BL279 see-also footer -->
## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/sessions-deep-dive](../howto/sessions-deep-dive.md)
- [howto/cost-report](../howto/cost-report.md)

---

# Cost report (GET /api/cost/report)

The grouped token + USD cost report over the daemon's recorded sessions — the
report engine shared by every surface (the CLI `datawatch cost report`, the
comm channel `cost report`, the MCP `cost_report` tool, the dashboard card).
Token-first: every row shows the group, its sessions and its tokens in/out; a
USD estimate appears only where a rate covers every backend in the group and
`cost_report.show_usd_estimates` is on (default true).

Query parameters (none required):

| Param | Values | Default |
|-------|--------|---------|
| `group_by` | `automaton` \| `automation` \| `backend` \| `release` \| `week` | `cost_report.default_group_by` (`automaton`) |
| `since` | RFC 3339 or `YYYY-MM-DD` | `until` minus `cost_report.default_window_days` (7) |
| `until` | RFC 3339 or `YYYY-MM-DD` | now |

Response fields:

| Field | Meaning |
|-------|---------|
| `rows[].key` | The group (Automaton name, Automation name, backend family, release, or week) |
| `rows[].sessions` | Sessions that landed in the group |
| `rows[].tokens_in` / `rows[].tokens_out` | Always present, always first |
| `rows[].usd_estimate` | Present only when a rate covers the whole group; absent (not zero) otherwise |
| `rows[].billing_mode` | `bundled_plan` \| `api_metered` \| `free_local` \| `mixed` (the group spans differently-priced providers) |
| `rows[].billing_label` | One line the whole UI family reuses, e.g. `bundled plan — API-equivalent, not billed` or `api_metered (assumed — no usage.billing entry)` |
| `rows[].api_equivalent_usd` | Bundled rows: the money equivalent at API rates — an equivalence, not a charge |
| `rows[].estimated_usage_points` | Bundled rows: the usage points that amount represents against the allowance |
| `rows[].share_of_monthly_price` | Bundled rows: the fraction of the plan's monthly price (0.05 = 5%) |
| `rows[].units` | `release` rows only: shipped units for that release (blank when the record has none) |
| `rows[].cache_read` / `rows[].cache_creation` / `rows[].tool_calls` | Cache tokens and tool calls |
| `rows[].first_activity` / `rows[].last_activity` / `rows[].span_seconds` / `rows[].active_seconds` | When the group worked, the span between, and the active time |
| `rows[].window_partial` / `rows[].window_note` | Set when the window cuts the group off (older than the ledger, or started mid-window) |
| `window_note` | Report-level note for the same |
| `attribution` | Attribution check: `automaton_launched`, `unattributed`, `session_ids`, `ok`, `note` |
| `retention_days` | The 31-day ledger the dollar figures rest on |
| `retention_note` | A human sentence for the same fact |
| `lifetime_fallback` | Whether some dollar figures rest on rates recorded outside that window |

`group_by release` attributes work to the release it shipped under; work whose
owner has not shipped yet lands under the `unreleased` row, and a group that
spans different releases carries `mixed` (`units` blank). Spend older than the
ledger is visible only through lifetime totals — the retention fields above
make that explicit.

The Automaton detail response (`GET /api/autonomous/prds/{id}`, every `detail`
level) carries a `cost` object: the same row the `group_by=automaton` report
gives that Automaton, over the whole ledger, with a zero row when it has no
spend.

Errors: `400` for a bad `group_by`, a bad date, or a `since` after `until`
(the message names the problem); `503` when autonomous mode is disabled.

```bash
curl -s 'http://127.0.0.1:8929/api/cost/report?group_by=backend&since=2026-10-01'
# → rows for each backend with tokens first and, where the profile says so,
#   the billing label and the API-equivalent point figures
```

---

# Billing profiles (`usage.billing`)

The per-provider billing mode behind the cost report's labels and the usage
calculator's point-value line. Set under `usage.billing.<provider>`, where
`<provider>` is the provider name from your LLM registry (any name without
dots, e.g. `claude-code`, `ollama`).

| Key | Accepts | Example |
|-----|---------|---------|
| `usage.billing.<provider>.mode` | `bundled_plan` \| `api_metered` \| `free_local` (unset = none) | `bundled_plan` |
| `usage.billing.<provider>.plan` | Free text label | `Claude Max 5x` |
| `usage.billing.<provider>.monthly_price` | Number ≥ 0 (`0` allowed for free plans) | `100` |
| `usage.billing.<provider>.currency` | 3-letter code | `usd` |
| `usage.billing.<provider>.reset_weekday` | `sunday`–`saturday` | `saturday` |
| `usage.billing.<provider>.reset_time` | `HH:MM` | `04:00` |
| `usage.billing.<provider>.allowance_unit` | `percent` \| `requests` \| `tokens` | `percent` |

Every field is optional and independent — set only what you know. A blank
string (or `0` for numbers) on a field clears it back to unset.

- **Read:** `GET /api/config` returns the block under `usage.billing`.
- **Write:** `PUT /api/config` with one field per request
  (`{"usage.billing.claude-code.mode": "bundled_plan"}`), the MCP `config_set`
  tool, the CLI `datawatch config set usage.billing.<provider>.<field> <value>`,
  or the comm channel `configure usage.billing.claude-code.mode=bundled_plan`.
- **Validation:** one invalid value rejects the whole call (400, the message
  names the key) and stores nothing; an unknown provider field is a 400.

A `bundled_plan` profile is what turns a cost-report row from a bare token
count into the label **"API-equivalent, not billed"** plus the three point
figures (API-equivalent USD, usage points, share of monthly price), and it is
where the usage calculator reads its point value (per point and per week) from.
An `api_metered` or `free_local` profile — or no profile at all, read back as
the assumed default — keeps the row's money figure as an ordinary estimate;
no profile is shown with the "(assumed — no usage.billing entry)" label.

