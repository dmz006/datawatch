# BL317 — Multi-Server PWA Plan

**Backlog item:** BL317  
**Version:** v7.4.0  
**Date filed:** 2026-05-18  
**GitHub issue:** #63  
**Depends on:** BL316 (federation peer registry must exist)  
**Sprint prefix:** T39  
**Test story range:** TS-604 – TS-643

---

## Goal

Extend the PWA so operators can manage multiple datawatch instances from a
single browser tab. Mirrors the Android client's v0.121.0+ multi-server
implementation and extends the BL312 server-picker chips (Sessions/Automata/
Alerts/Observer/Dashboard) with full fan-out, per-row server attribution, and
an "All servers" sentinel that aggregates across every registered server.

---

## Background

BL312 (v7.2.0) shipped:
- Runtime multi-server registry (`/api/servers` CRUD)
- Per-tab server picker chips (All / Local / server names)
- Sessions all-servers mode (`GET /api/sessions/aggregated`)
- Automata + Alerts all-servers mode (fan-out aggregated endpoints)
- Observer + Dashboard single-server picker (proxy routing)

BL317 extends these with:
- Cross-server session input routing (enter text into a remote session from any tab)
- Fan-out to federation peers (not just static YAML servers)
- All-servers mode across all remaining views
- Profile fan-out (run an automaton on all servers simultaneously)
- Per-row server badge on all aggregated views

---

## Architecture

```
PWA (single browser tab)
│
├── Server picker: [All] [Local] [server-A] [server-B] [fed-peer-X]
│                         ↕ activeServer state
│
├── Sessions view ─── All → GET /api/sessions/aggregated → fan-out
│                         ↕ per-row: server badge
│                         ↕ input: proxy POST to /api/servers/{name}/proxy/sessions/{id}/input
│
├── Automata view ─── All → GET /api/autonomous/prds/aggregated → fan-out
│                         ↕ start on server: POST to /api/servers/{name}/proxy/autonomous/prds
│
├── Alerts view ───── All → GET /api/alerts/aggregated → fan-out
│
├── Observer view ─── Single server only (keeps existing picker)
│
└── Dashboard view ── Single server only (keeps existing picker)
```

---

## Sprint plan

### S1 — Fan-out to federation peers + server badge polish

**Scope:**

1. **Extend aggregated endpoints to include federation peers** — `GET /api/sessions/aggregated`, `GET /api/autonomous/prds/aggregated`, and `GET /api/alerts/aggregated` currently fan out to `cfg.Servers`. Extend each to also fan out to `serverStore.ListFederated()` entries (those with `Enabled: true`). The federation peer's token is used as the Bearer token for the outgoing fetch.

2. **Per-row server attribution** — ensure every item in aggregated responses has a `server` field. Session cards, PRD cards, and alert rows must display the server badge consistently. Fix any cases where the badge is missing or shows "local" incorrectly.

3. **Server picker: federation peers visible** — the chip bar currently reads from `GET /api/servers`. Ensure federation peers (Federated=true entries) appear in the chip bar with a distinct visual treatment (e.g. federation icon or "⛓" prefix).

4. **Cross-server session input** — when `activeServer` is a remote server, the "send input" action (text box / MsgSendInput / `/send` comm command) must proxy through `POST /api/sessions/{peer_name}/{session_id}/input` on the local daemon, which then forwards to the peer. (BL316 S2 implements the proxy endpoint; BL317 S1 wires the PWA to use it.)

**Test stories (T36, TS-550 – TS-564):**

| Sprint | ID | Surface | Test |
|---|---|---|---|
| T36 | TS-550 | REST | GET /api/sessions/aggregated includes entries from federation peers |
| T36 | TS-551 | REST | GET /api/autonomous/prds/aggregated includes entries from federation peers |
| T36 | TS-552 | REST | GET /api/alerts/aggregated includes entries from federation peers |
| T36 | TS-553 | REST | Each aggregated item has server field |
| T36 | TS-554 | PWA | Server picker shows federation peers with ⛓ icon |
| T36 | TS-555 | PWA | Sessions view All mode shows cards from federation peers with server badge |
| T36 | TS-556 | PWA | Input on remote session proxies through /api/sessions/{peer}/{id}/input |
| T36 | TS-557 | PWA | Automata All mode shows PRDs from federation peers |
| T36 | TS-558 | PWA | Alerts All mode shows alerts from federation peers |
| T36 | TS-559 | MCP | list_sessions MCP tool result includes server field on each item |
| T36 | TS-560 | CLI | datawatch session list --all-servers includes remote sessions |
| T36 | TS-561 | Comm | "sessions all" comm command returns aggregated list with server field |
| T36 | TS-562 | Locale | 5 bundles contain server_picker_federation_peer key |
| T36 | TS-563 | Locale | 5 bundles contain session_server_badge key |
| T36 | TS-564 | Mobile | datawatch-app issue filed for multi-server session input routing |

---

### S2 — All-servers mode for remaining views + profile fan-out

**Scope:**

1. **Profile fan-out** — add a "Run on all servers" option to the Automata launch modal. When selected, `POST /api/autonomous/prds` is called in parallel on every enabled server. Results aggregated and shown per-server.

2. **Observer all-servers mode** — the Observer tab currently supports only single-server. Add an "All peers" mode that merges `GET /api/observer/stats` from all servers into a unified view.

3. **Dashboard all-servers mode** — allow dashboard cards that support aggregation (sessions sparklines, alert feed) to pull from all servers when "All" is active.

4. **Server management card in Settings** — merge the existing "Remote Servers" settings card (BL312 S2) with the new "Federation Peers" card (BL316 S2) into a unified "Connected Instances" card showing both server types with clear type labels ("server" vs "federation peer").

**Test stories (T36, TS-565 – TS-579):**

| Sprint | ID | Surface | Test |
|---|---|---|---|
| T36 | TS-565 | PWA | Automata launch has "Run on all servers" option |
| T36 | TS-566 | REST | POST to multiple servers in parallel → each returns 201 |
| T36 | TS-567 | PWA | Observer All peers mode shows merged stats |
| T36 | TS-568 | PWA | Dashboard sessions sparkline in All mode aggregates across servers |
| T36 | TS-569 | PWA | Settings → Connected Instances card shows servers + federation peers unified |
| T36 | TS-570 | PWA | Federation peer row in Connected Instances shows capabilities chips |
| T36 | TS-571 | MCP | federation_sessions MCP tool (GET /api/federation/sessions) includes runtime peers |
| T36 | TS-572 | CLI | datawatch federation sessions exits 0 |
| T36 | TS-573 | Locale | 5 bundles contain connected_instances_title key |
| T36 | TS-574 | Locale | 5 bundles contain federation_peer_type_label key |
| T36 | TS-575 | Mobile | datawatch-app issue filed for profile fan-out |

---

### S3 — Polish + offline handling + mobile parity

**Scope:**

1. **Offline server handling** — when a server is unreachable in All mode, show a "server unreachable" inline error in that server's card/row section rather than a global error. Existing `errors` map in `FederationResponse` already carries per-server errors; PWA must surface them per-section.

2. **Server health indicator** — add a health dot (green/amber/red) next to each server in the chip bar, updated by polling `GET /api/servers/health`.

3. **Cross-server session resume** — when a session on a remote server is resumed from the PWA, the resume request is proxied to the correct server.

4. **Mobile parity** — file comprehensive datawatch-app issue for all BL317 features.

**Test stories (T36, TS-576 – TS-589):**

| Sprint | ID | Surface | Test |
|---|---|---|---|
| T36 | TS-576 | PWA | Unreachable server shows inline error in All mode (not global) |
| T36 | TS-577 | PWA | Server chip bar shows health dot per server |
| T36 | TS-578 | REST | GET /api/servers/health returns per-server health array |
| T36 | TS-579 | PWA | Resume on remote session proxies to correct server |
| T36 | TS-580 | Comm | "servers health" comm command returns health array |
| T36 | TS-581 | MCP | server_health MCP tool returns per-server health |
| T36 | TS-582 | CLI | datawatch server health exits 0 |
| T36 | TS-583 | Locale | 5 bundles contain server_health_ok key |
| T36 | TS-584 | Locale | 5 bundles contain server_health_unreachable key |
| T36 | TS-585 | Mobile | datawatch-app issue filed for server health indicator |
| T36 | TS-586 | Mobile | datawatch-app issue filed for cross-server session resume |
| T36 | TS-587 | REST | Cross-server session resume proxied through /api/servers/{name}/proxy |
| T36 | TS-588 | Smoke | release-smoke.sh §multiserver section covers All-servers aggregation |
| T36 | TS-589 | Docs | docs_search "multi-server all servers mode" returns multi-server.md |

---

## 7-surface parity tracking

| Surface | S1 | S2 | S3 |
|---|---|---|---|
| REST API | Fan-out to fed peers, cross-host input | Profile fan-out, observer/dashboard all | Server health endpoint |
| MCP | list_sessions with server field | federation_sessions | server_health |
| CLI | session list --all-servers | federation sessions | server health |
| Comm | sessions all | servers health | — |
| PWA | Picker shows peers, All mode, cross-server input | Profile fan-out, unified Connected Instances | Health dots, offline error, resume proxy |
| Locale × 5 | server_picker_federation_peer, session_server_badge | connected_instances_title | server_health_ok, server_health_unreachable |
| Mobile | Issue for multi-server input | Issue for profile fan-out | Issues for health + resume |

---

## File map

| File | Purpose |
|---|---|
| `internal/server/federation.go` | Extend fan-out to serverStore.ListFederated() |
| `internal/server/api.go` | Cross-host session input proxy + server health endpoint |
| `internal/server/web/app.js` | Server picker federation peers, All-mode views, health dots |
| `internal/server/web/locales/*.json` | New locale keys |
| `internal/mcp/bl317_multiserver_tools.go` | MCP tools for S2-S3 |
| `cmd/datawatch/cli_federation.go` | federation sessions CLI |
| `docs/howto/multi-server.md` | Operator howto |

---

## Acceptance (v7.4.0 full release)

- All TS-550–TS-589 pass against live test daemon
- `go test ./...` green
- Smoke §multiserver section fully covered
- `docs/howto/multi-server.md` present and indexed
- 7-surface parity table fully checked
- Mobile issues filed in datawatch-app for all surfaces
- No aggregated view missing per-row server attribution

---

## Rules audit template (fill per sprint)

| Rule | S1 | S2 | S3 |
|---|---|---|---|
| DIP | — | — | — |
| 7-surface parity | Partial | Full | Full |
| Error-Filing Rule | — | — | — |
| Android Sync Rule | Issue filed | Issues filed | Issues filed |
| Localization Rule | × 5 bundles | × 5 bundles | × 5 bundles |
| Smoke | §fed | §multiserver | §multiserver complete |
| No internal refs | ✓ | ✓ | ✓ |
| Howto-Coverage | — | multi-server.md | — |
| Cookbook | TS-550–564 | TS-565–575 | TS-576–589 |
| Plans folder hygiene | This doc dated 2026-05-18 | — | — |
