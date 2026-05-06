# Release Notes — v3.7.0 (Sprint S3 — Cost + Observability)

**2026-04-19.** Three items shipped: BL6 cost tracking, BL86 remote
GPU/system stats agent (**new binary**), BL9 operator audit log.

---

## Highlights

- **BL6 — Cost tracking.** `Session` gained `tokens_in`, `tokens_out`,
  `est_cost_usd`. Per-backend rate table (`session.DefaultCostRates`)
  with operator override via `Manager.SetCostRates`. New endpoints
  `GET /api/cost` and `POST /api/cost/usage` for rollups + ingestion.
- **BL86 — `datawatch-agent` binary.** First standalone product
  binary: `cmd/datawatch-agent/main.go`. Exposes
  `GET /stats` (GPU via `nvidia-smi`, CPU/memory/disk via
  `/proc` + `free` + `df`) and `GET /healthz`. Cross-built for
  `linux-amd64` + `linux-arm64`. Operators install on remote Ollama /
  GPU hosts.
- **BL9 — Operator audit log.** Append-only JSON-lines log under
  `<data_dir>/audit.log`. `GET /api/audit` with filters (actor,
  action, session_id, since/until, limit). Newest-first ordering.

---

## API additions

```
GET  /api/cost                              # BL6 — aggregate (or ?session=)
POST /api/cost/usage                        # BL6 — record usage delta
GET  /api/audit?actor=&action=&session_id=&since=&until=&limit=N   # BL9
```

`Session` JSON additions (omitempty):
```json
{ "tokens_in": 0, "tokens_out": 0, "est_cost_usd": 0 }
```

---

## New binary: `datawatch-agent`

The agent is a single Go binary (~10 MB) with no dependencies beyond
the system tools it queries (`nvidia-smi` optional, `free` and `df`
required, `/proc` for cpuinfo+loadavg).

```bash
# install on the remote GPU host
curl -fsSL https://github.com/dmz006/datawatch/releases/download/v3.7.0/datawatch-agent-linux-amd64 \
  -o /usr/local/bin/datawatch-agent
chmod +x /usr/local/bin/datawatch-agent
datawatch-agent --listen 0.0.0.0:9877
```

Then point datawatch at it (config field is the existing
`ollama.host`-adjacent surface; full config wiring lands in v3.7.1).

---

## Container images

| Image | Change | Action |
|---|---|---|
| `parent-full` | Daemon adds new endpoints | **Rebuild required** |
| All other images | No change | No rebuild |

datawatch-agent does not yet have a container image — it ships as a
bare binary in v3.7.0; agent-base wrap is a follow-up.

Helm: `version: 0.9.0`, `appVersion: v3.7.0`.

---

## Testing

- **1079 tests / 48 packages** all passing (+23 vs. v3.6.0).
- New audit package: 7 tests in `internal/audit/log_test.go`.
- BL6: 7 tests in `internal/session/bl6_cost_test.go` + 5 REST tests.
- BL9: 4 REST tests in `internal/server/bl9_audit_test.go`.

---

## Upgrading from v3.6.0

```bash
datawatch update                                            # single host
helm upgrade dw datawatch/datawatch -n datawatch \
  --set image.tag=v3.7.0                                    # cluster
```

---

## What's next

**Sprint S4 (v3.8.0) — Messaging + UI polish**: BL15 rich previews,
BL31 device targeting, BL69 splash logo, BL42 quick-response. ~3 days.
