---
docs:
  index: true
  topics: [audit, access-log, security, federation, siem]
exec_params: []
exec_steps:
  - tool: audit_query
    description: Query the operator action log
    args: {limit: 20}
    read_only: true
  - tool: audit_access_query
    description: Query the HTTP access / auth-failure log
    args: {limit: 20}
    read_only: true
---
# How-to: Audit & Access Logging

datawatch keeps two separate, append-only logs of "who did what" —
one for deliberate operator actions, one for every single request
that reaches the daemon. Both are queryable live and can mirror to a
SIEM.

## What it is

Two JSON-lines files under `data_dir` (`~/.datawatch` by default):

- **`audit.log`** — the *operator action* log. One entry per
  state-changing action: session start/kill/delete/rollback/
  send_input, Automata/PRD lifecycle (create, approve, reject,
  cancel, every `set_*` config change, ...), alert-rule and
  federation-peer changes, device registration, schedules,
  orchestrator graphs, and raising an alert via `create_alert`. Each
  entry names the action, the resource, and the real caller identity
  (`admin`, `session:<id>`, or `peer:<name>` — never a hardcoded
  "operator" string, and never the bearer token).
- **`access.log`** — every HTTP request, auth failure, and WebSocket
  connect/disconnect, from **all three** of CLI, REST, and MCP. They
  all end up as HTTP clients against the same `/api/*` surface behind
  one auth middleware, so one log covers all three — there's no
  separate wiring per surface.

Neither log ever records the `Authorization` header or a raw
token/nonce value.

**Not covered by either log**: alerts (`alerts.json`, see
[alerts-and-notifications.md](alerts-and-notifications.md)) are a
separate persistence mechanism and are not SIEM-mirrored the way this
page's two logs are, except incidentally for the one REST path that
happens to also write an `audit.log` entry. See that page's "Raising
an alert yourself" section for the exact boundary. The spawned-agent
audit trail (`GET /api/agents/audit`, `agent_audit` MCP tool,
`ParentAgentID`-filterable) is also separate — it's about spawned
remote-agent-cluster workers, not this daemon's own HTTP surface.

## Base requirements

- `datawatch` daemon running and reachable at `https://<host>:8443`.
- An admin bearer token, to read either log (`audit:read` capability
  — a federation peer needs it granted explicitly, same as any other
  read capability).

## Setup

Both logs are on by default. Three config keys, all under `audit:`:

```yaml
# ~/.datawatch/datawatch.yaml
audit:
  access_log_enabled: true   # default true
  retention_days: 30         # default 30; negative = never prune
  cef_mirror_enabled: false  # default false — opt in for SIEM forwarding
```

Same keys work from `PUT /api/config`, the MCP `config_set` tool, and
Settings → Audit & Access Log in the PWA.

Pruning (dropping entries older than `retention_days`) runs once at
daemon startup and on a 24h ticker — no separate cron job needed.

## Query the logs

### Via REST

```sh
export BASE=https://localhost:8443
export TOKEN=<your_bearer_token>

# Operator action log — last 20 entries.
curl -sk -H "Authorization: Bearer $TOKEN" "$BASE/api/audit?limit=20"

# Filter to one action type or actor.
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/audit?action=rollback"
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/audit?actor=peer:daemon-a"

# Access log — every successful request + auth failure.
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/audit/access?action=http_access&limit=20"
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/audit/access?action=auth_failure"
```

Both endpoints share the same filter shape: `actor`, `action`,
`since`/`until` (RFC3339), `limit`.

### Via MCP

| Tool | Description |
|------|--------------|
| `audit_query` | Query the operator action log — same filters as REST |
| `audit_access_query` | Query the access/auth-failure log — same filters as REST |

```
audit_query action=rollback limit=10
audit_access_query action=auth_failure
```

### Example — new IPs and auth failures in the last day

```sh
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/audit/access?action=http_access&since=$(date -u -d '1 day ago' +%Y-%m-%dT%H:%M:%SZ)" \
  | jq -r '.entries[].details.remote_ip' | sort -u

curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/audit/access?action=auth_failure" | jq '.entries'
```

## SIEM forwarding (CEF)

Set `audit.cef_mirror_enabled: true` and restart. Every write to
`audit.log`/`access.log` also appends a CEF-formatted line to
`audit.log.cef`/`access.log.cef` alongside it — point your Splunk /
QRadar / ArcSight / Sentinel forwarder at the `.cef` files. The
JSON-lines files stay the source of truth for `/api/audit`/
`/api/audit/access`'s own query surface; CEF is an additive mirror,
not a format switch.

```yaml
audit:
  cef_mirror_enabled: true
```

## Metrics

- `datawatch_access_log_events_total{action,principal_kind}`
  (Prometheus, `/metrics`) — durable counter, survives restarts.
  `principal_kind` is the *coarse* category (`admin`, `session-scoped`,
  `peer`, `proxy`, `unauthenticated`) — never the exact peer/proxy
  name, to keep the label set bounded.
- `access_log_events_total` / `access_log_auth_failures_total` on
  `GET /api/stats` — in-process counts, reset on restart.

## Cross-hop attribution (federation)

A request forwarded to this daemon by another datawatch daemon acting
as a federation peer — LLM delegation, `/api/proxy/{name}/...`, or
`/remote/{name}/...` — normally logs as `peer:<name>` in `access.log`:
that only tells you *which peer* presented the request, not *who
behind that peer* actually initiated it on their own daemon.

If the forwarding daemon sends a verified `X-Datawatch-Hop-Chain`
header, the access-log entry also carries two extra detail fields:

- `details.origin_actor` — who initiated the action on the
  *forwarding* daemon (e.g. `admin` or `session-scoped`).
- `details.hop_chain` — the full ordered list of daemons the request
  passed through.

### Trust model

The chain is **HMAC-signed per hop**, using each pair of daemons'
*existing* shared federation-peer bearer token — not new asymmetric
keys. Each daemon verifies only the link signed with the token it
directly shares with its sender; it does not re-verify the whole
chain back to the origin. That's a deliberate tradeoff, not a gap: a
peer that already holds a real shared token for some hop already has
full access at that trust level today, so a forged or dropped chain
never grants *more* than the peer already had — a chain that fails
verification is simply dropped and logged as
`details.hop_chain_invalid: true`, falling back to the plain
`peer:<name>` attribution. A peer running an older version that
doesn't send a chain at all gets that same plain attribution.

```sh
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/audit/access?action=http_access" \
  | jq '.entries[] | select(.details.origin_actor != null) |
        {actor, origin_actor: .details.origin_actor, hop_chain: .details.hop_chain}'
```

## Diagram

```
  Every request (CLI / REST / MCP — all three are HTTP clients
  against the same /api/* surface)
         │
         ▼
  fedAuthMiddleware  (the one choke point)
         │
         ├─ resolves principal: admin | session:<id> | peer:<name> | proxy:<name>
         │
         ├─ peer branch only: verify X-Datawatch-Hop-Chain
         │     valid   → attach origin_actor + full hop_chain
         │     invalid → drop it, flag hop_chain_invalid:true
         │     absent  → nothing to attach (older peer)
         │
         ├──────────────► access.log   (this request, always)
         │                   + access.log.cef   (if cef_mirror_enabled)
         │
         ▼
      handler runs
         │
         └─ state-changing action? ──► audit.log   (action + resource + real actor)
                                           + audit.log.cef   (if cef_mirror_enabled)

  Query surface (both logs):
    GET /api/audit            │  MCP audit_query
    GET /api/audit/access     │  MCP audit_access_query
```

## Common pitfalls

- **`access_log_events_total` on `/api/stats` resets to 0 after a
  restart.** That field is in-process only. Use the Prometheus
  counter (`datawatch_access_log_events_total`) for a durable count
  across restarts.
- **`GET /api/audit/access` returns 503.** The access log isn't wired
  — check `audit.access_log_enabled` is `true` (it is by default; a
  prior `PUT /api/config` may have disabled it).
- **CEF files exist but my SIEM shows duplicate/stale data.** CEF is
  append-only like the JSON-lines file; retention pruning rewrites
  `audit.log`/`access.log` in place but does **not** prune the `.cef`
  mirror files — point log rotation at those separately if volume
  matters.
- **`origin_actor` never appears even though the caller says they're
  forwarding.** Confirm the forwarding daemon is new enough to send
  `X-Datawatch-Hop-Chain` at all, and that it's actually forwarding
  through one of the 4 wired call sites (LLM delegation,
  `handleProxyWS`, `handleAggregatedSessions`, `handleRemotePWA`) —
  not every cross-daemon call forwards attribution yet.
- **I expected alerts to show up here too.** They don't, except
  incidentally — see "Not covered by either log" above and
  [alerts-and-notifications.md](alerts-and-notifications.md).

## See also

- [howto/federation-cbac](federation-cbac.md) — capability grants;
  `audit:read` gates both query endpoints for a federation peer.
- [howto/alerts-and-notifications](alerts-and-notifications.md) — the
  separate, not-SIEM-integrated alert store.
- [howto/secrets-manager](secrets-manager.md) — a related, separately
  audited subsystem (secret reads/writes have their own entries in
  `audit.log`).
- [datawatch-definitions](../datawatch-definitions.md)
