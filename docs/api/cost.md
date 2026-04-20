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
