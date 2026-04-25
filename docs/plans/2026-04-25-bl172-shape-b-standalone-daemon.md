# BL172 — Sprint S11: Shape B standalone observer daemon

**Status:** Design — implementation pending
**Filed:** 2026-04-25
**Predecessor:** [BL171 / S9 v4.1.0 — observer substrate + Shape A](2026-04-22-bl171-datawatch-observer.md)
**Target release:** v4.4.0 (S11 was originally planned for v4.2.0; slipped while v4.2.0 / v4.3.0 absorbed PWA UX + BL174 + BL170 work)
**GitHub:** [#20](https://github.com/dmz006/datawatch/issues/20)

## Context

S9 (v4.1.0) shipped the observer substrate + Shape A (in-process plugin):
the `internal/observer/` collector, `StatsResponse v2`, the `/api/observer/*`
REST surface, MCP tools, CLI parity, the eBPF capability probe, and the PWA
Settings card. Mobile then consumed the same wire contract.

What's missing is **Shape B** — a separate `datawatch-stats` binary that
runs on hosts where the full datawatch parent isn't appropriate (Ollama
boxes, GPU inference machines, lightweight mobile-edge nodes) and pushes
its `StatsResponse v2` to a primary datawatch over HTTPS for federated
aggregation.

This doc refines the BL171 sketch into an implementation plan.

## Scope

In scope:
- New binary `cmd/datawatch-stats/` reusing `internal/observer/` end-to-end.
- Peer registration + HMAC token mint flow against an existing parent.
- 5 s push of `StatsResponse v2` to `POST /api/observer/peers/{name}/stats`.
- Parent-side `/api/observer/peers/*` endpoints + roll-up into the main
  envelope set.
- Systemd unit (linux), launchd plist (macOS), Homebrew formula (macOS).
- `datawatch setup ebpf` extended to recognise the `datawatch-stats`
  systemd unit + drop-in.
- PWA Settings → Monitor → Peers row (live; the v4.1.0 stub is list-only).

Out of scope (deferred to S12):
- Cluster container shape (Shape C — Helm chart, k8s manifest).
- Cross-cluster federation tree.
- Per-peer alert routing.

## Wire contract (frozen by BL171)

`StatsResponse v2` is unchanged. The peer pushes the **same payload** the
in-process plugin produces, with three additional top-level fields populated
on push:

```json
{
  "v": 2,
  "shape": "B",
  "peer_name": "ollama-box",
  "peer_token_id": "phk_a1b2…",   // first 8 chars of token id (audit only)
  "host": { … },
  "envelopes": [ … ],
  …
}
```

Parent rejects pushes with mismatched shape/peer_name pairs and stale
HMAC signatures (5 s clock skew tolerance, replay-window 30 s).

## Deliverables

### 1. `cmd/datawatch-stats/` binary

Layout:
```
cmd/datawatch-stats/
  main.go         — flag parsing, peer-loop, observer reuse
  config.go       — minimal yaml: parent, name, listen_addr, ebpf_enabled
  peer.go         — registration + token persist + push loop
  main_test.go    — registration / push happy path + token-rotation
```

Flags:
```
--datawatch <url>       primary datawatch URL (required)
--name <peer-name>      stable peer name; defaults to hostname
--config <path>         optional yaml override (~/.datawatch-stats/config.yaml)
--token-file <path>     where to persist the minted token (default
                        ~/.datawatch-stats/peer.token, mode 0600)
--push-interval <dur>   default 5s; min 1s
--listen <addr>         optional local /api/stats endpoint for sidecar reads;
                        default off (push-only)
```

Lifecycle:
1. Parse flags + load config.
2. Construct `internal/observer.Collector` (same code path as Shape A).
3. Read `--token-file`. If absent, register: `POST /api/observer/peers`
   with `{name, host_info, version}`. Parent returns `{token, token_id}`.
   Write to disk 0600.
4. Start collector tick.
5. Push loop: every `push_interval`, snapshot the latest `StatsResponse v2`,
   sign with the token (HMAC-SHA256 over `<unix_ms>.<sha256(body)>`),
   send to `POST /api/observer/peers/{name}/stats` with headers
   `X-Datawatch-Peer-Sig: <hex>` + `X-Datawatch-Peer-Ts: <unix_ms>`.
6. On 401: re-register (token rotation supported by parent).
7. On signal: drain push queue + exit.

### 2. Parent-side endpoints

```
POST   /api/observer/peers                     register
GET    /api/observer/peers                     list (S9 stub already exists)
DELETE /api/observer/peers/{name}              de-register
POST   /api/observer/peers/{name}/stats        push
GET    /api/observer/peers/{name}/stats        last-known snapshot
```

`internal/observer.PeerRegistry` (new) — in-memory map keyed by
`peer_name` with `{token_hash, last_push, last_payload}`. Persisted to
`<data_dir>/observer/peers.json` so a parent restart doesn't drop the
peers (peer-side will re-auth via re-register on the first 401).

The aggregator already merges the in-process envelope set; extend
`/api/observer/envelopes` and `/api/observer/stats` to also fold in
`peers[*].envelopes` (with `peer_name` annotation).

### 3. Distribution

- `deploy/systemd/datawatch-stats.service` — User= datawatch, Restart=
  on-failure, ExecStart with `--datawatch` and `--name` from
  `/etc/datawatch-stats/env`.
- `deploy/launchd/com.datawatch.stats.plist` — equivalent for macOS.
- `deploy/homebrew/datawatch-stats.rb` — Homebrew formula (taps the same
  release page).
- `Makefile` target `cross-stats` mirroring `cross-channel` — 5 platform
  binaries.

### 4. setup ebpf parity

`datawatch setup ebpf` learns to find both unit names. The drop-in path
becomes `/etc/systemd/system/<unit>.d/ebpf.conf`; same body, just two
units instead of one. `datawatch setup ebpf --target stats` forces
the standalone-daemon unit when both are installed.

### 5. PWA Settings → Monitor → Peers

The v4.1.0 stub already lists peers (read-only, fed by `/api/observer/peers`).
S11 adds:
- Peer health dot (green / amber / red based on last-push staleness).
- Click-through to a per-peer envelope drill-down (reuses the existing
  envelope-detail panel — just scoped to that peer's payload).
- Manual "rotate token" + "remove" actions.

### 6. Tests

- Unit: `peer.go` registration + token persist + push signature.
- Unit: `PeerRegistry` add / lookup / token-rotate / persistence.
- Integration: spin up an httptest parent, run `datawatch-stats` against
  it for 3 ticks, assert the parent saw 3 pushes and the envelopes are
  reachable via `/api/observer/envelopes?peer=<name>`.

## Auth model

HMAC-SHA256 only. No mTLS in S11 (defer to S12 cluster shape, where
operators have a cert-management pipeline).

Token lifecycle:
- Mint on first `POST /api/observer/peers` — random 256-bit, base64.
- Token stored on parent as `bcrypt(token)` to avoid plaintext at rest.
- Peer stores plaintext at `~/.datawatch-stats/peer.token` 0600.
- Operator-initiated rotation: `DELETE /api/observer/peers/{name}` on
  parent → next push gets 401 → peer re-registers automatically.

Replay window: 30 s. Clock skew tolerance: 5 s. Window enforced via the
`X-Datawatch-Peer-Ts` header — anything outside window → 401 + audit
log line.

## Migration

None — Shape B is additive. Existing operators see no change unless they
explicitly install `datawatch-stats` on a peer host.

## Open questions

1. **Shape B + Shape A on the same host?** Allowed but redundant. If both
   register to the same parent, the in-process plugin's envelopes are
   considered authoritative; the peer's envelopes for the same hostname
   are dropped on roll-up with an audit-log warning. Operator can
   override via `observer.peer_priority: peer | local`.
2. **Push backpressure** — what if the parent is unreachable for minutes?
   Peer-side ring buffer (default 60 snapshots = 5 min at 5 s) so the
   parent gets the gap-fill on reconnect.
3. **Multi-parent** — out of scope; one peer pushes to one parent. Peers
   register in a tree if a multi-parent topology is needed (defer to S13).

## Sprint plan (5 tasks, ~3 days)

| # | Task | Notes |
|---|---|---|
| 1 | `cmd/datawatch-stats/` skeleton + flag parsing + observer reuse | reuses `internal/observer.Collector` whole |
| 2 | Parent `/api/observer/peers/*` handlers + `internal/observer.PeerRegistry` + persist | in-memory + JSON file |
| 3 | Peer registration + HMAC sign + push loop + token rotation | 3 unit tests |
| 4 | systemd unit + launchd plist + Homebrew + `cross-stats` Makefile target | shippable artifacts |
| 5 | PWA Peers row upgrade (health dot, drill-down, rotate/remove actions) | reuses existing envelope panel |

Each lands as an independent commit; PR aggregator at the end.

## Acceptance criteria

- [ ] `datawatch-stats --datawatch https://parent:8443 --name ollama-box`
      runs cleanly, registers, and the parent's
      `/api/observer/peers` lists it within one push interval.
- [ ] Parent's `/api/observer/envelopes?peer=ollama-box` returns the
      peer's envelopes within 10 s of registration.
- [ ] PWA Settings → Monitor → Peers shows the peer with a green dot;
      click drills into the per-peer panel.
- [ ] Killing + restarting `datawatch-stats` reuses the persisted
      token; killing + restarting the **parent** triggers a single
      401 + automatic re-register.
- [ ] Cross-built artifacts published in the v4.4.0 release.
