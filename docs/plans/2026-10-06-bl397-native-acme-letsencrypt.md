# Plan: BL397 — Native ACME/Let's Encrypt subsystem in datawatch

- **Date**: 2026-10-06
- **Version at planning**: v8.61.9
- **Status**: Planned — not started. Scoped via operator interview (this doc
  records the decisions; see §Decisions log).
- **Source PRD**: `74e9eb05` ("Let's Encrypt integration research for public
  datawatch deployments"), research-only, completed 2026-10-06. Output docs:
  `/home/dmz/workspace/datawatch-letsencrypt/docs/01-le-options-landscape.md`
  through `06-comparison-recommendations.md`. No datawatch code was written by
  that PRD — this plan is the build that follows from its research.

## Context

The PRD researched three solutions for getting a browser-trusted TLS cert
onto a public datawatch deployment:

- **Plan A** — HTTP-01 via an external reverse proxy (Caddy). Zero datawatch
  code; Caddy is the issuer. datawatch stays a cert *consumer*.
- **Plan B** — DNS-01, either a delegated subzone (B1) or a scoped DNS
  provider API token (B2). Needed for wildcards or when port 80 is blocked;
  requires either zone delegation or a standing provider credential — i.e.
  DNS management.
- **Plan C** — a native `acme.Manager` subsystem *inside* the datawatch
  daemon. datawatch itself performs the ACME handshake, holds the account
  key, and runs the renewal loop. This is the only option where "the
  operator never leaves datawatch for certificate management."

The operator's ask — "datawatch should support enabling letsencrypt on a
url and support the full handshake and scheduled re-auth and maintenance
while it is active," explicitly declining DNS management — maps directly
onto **Plan C**, using the **HTTP-01** validation method (Plan C's `dns01`
mode is Plan B's DNS-01 mechanics wrapped in the same subsystem, and is
out of scope here per the decision log below).

## Decisions log (operator interview, 2026-10-06)

1. **Solution scope: Plan C only.** Plan A and Plan B are researched,
   understood, and explicitly declined for this build — not implemented,
   not stood up as a stopgap. If a future constraint appears (port 80
   blocked, wildcard required), B's mechanics are still available as Plan
   C's phase 2 (`acme.method: dns01`), not a separate build.
2. **Cert-apply mechanism: restart-based (Phase 1), not hot-swap (Phase 3).**
   A cert renewal triggers `server.auto_restart_on_config: true` — the
   daemon restarts itself, re-loading the new PEM at TLS-listener startup.
   One short window per issue/renewal (~every 90 days). Zero-downtime
   hot-swap (phase 3: drop `tls_cert`/`tls_key` from `RESTART_FIELDS`,
   listener reloads PEM on mtime change) is **deferred**, not declined —
   worth revisiting once phase 1 is proven.
3. **ACME client library: `github.com/go-acme/lego/v4`.** New third-party Go
   dependency. Handles JWS signing, account registration, order state
   machine, and the HTTP-01 challenge handler. AGENT.md's B17 (72-hour rule
   on new deps, `go mod tidy` run, CHANGELOG dependency note) applies at
   implementation time.
4. **Surface scope: all 7 together, v1.** YAML config (`acme:` block), REST
   (`/api/acme/*`), MCP tools (`acme_status`/`acme_renew`/`acme_issuer_log`),
   CLI (`datawatch acme status|renew|verify`), comm verbs (`!acme
   status|renew|verify`), PWA Settings card (TLS / Auto-renewal), and
   audit/alerts (`AlertSink` → `daemon-app.log` + `alerts.json` +
   alert-center). No deferred fast-follow — ships together.
5. **ACME endpoint: staging-first.** `acme.endpoint: staging` is the config
   default. The full issue → apply → renew → alert flow is verified against
   staging (untrusted certs, generous rate limits) before a single config
   flip to `production` for a real browser-trusted cert.
6. **Test target: `spaceportsouth.dmzs.com` → `66.228.59.180`** (named for
   the place Trent's people were destroyed, in Daniel Keys Moran's *The Long
   Run* — the novel the project is named after: "The DataWatch sees
   everything"). DNS A record being added by the operator directly; **no
   DNS management is implemented by this plan** — the daemon only ever
   consumes a hostname that's already resolvable, never creates or edits a
   DNS record.
   - **Shared-resource constraint**: this VM is also in active use by the
     `datawatch-app` session as the Apple App Store sandbox for the iOS
     TestFlight/release pipeline. Confirmed via a pre-flight SSH check
     (2026-10-06): Ubuntu 26.04, ports 80/443 free, outbound HTTPS to
     `acme-v02.api.letsencrypt.org` reachable (200). **Coordinate with the
     `datawatch-app` session before binding port 80/installing datawatch
     there** — do not assume continued exclusive access; re-verify port
     80/443 are still free immediately before the live test, not just at
     planning time.

## Architecture (per PRD doc 05, §3)

```
acme.Manager (new subsystem)
 ├─ Account    — ACME account state (ECDSA P-256 key, directory, agreement)
 ├─ OrderState — per-order: authz, challenge, finalization (24h TTL)
 ├─ Renewer    — background goroutine, checks expiry every 6h,
 │               re-orders when remaining life <= renewal_days
 ├─ Apply      — writes PEM, points server.tls_cert/mcp.tls_cert,
 │               triggers auto_restart_on_config
 └─ AlertSink  — every state change → alerts + audit
```

**Config schema** (`acme:` top-level block, same shape as `server:`/`mcp:`):

```yaml
acme:
  enabled: false
  endpoint: staging          # staging (default) | production
  domains:
    - spaceportsouth.dmzs.com
  method: http01             # http01 only for this build; dns01 is phase 2, out of scope
  http01:
    listen: ""                # default: dedicated 127→0.0.0.0:80 bridge
  renewal_days: 30            # renew when remaining life <= N days (90-day LE certs;
                               # corrected 2026-10-06 from the PRD's proposed 66 — that
                               # value renews at only 24 days of cert age, far more
                               # aggressive than certbot/Caddy's ~30-days-remaining norm)
  retry:
    interval_minutes: 30
    max_consecutive_failures: 5
  apply:
    hot_swap: false            # phase 3, not this build
    update_mcp_cert: true
```

**Storage** (per PRD doc 05, §3.2 — reusing existing datawatch encryption
conventions, `docs/encryption.md`):
- ACME **account private key** → `{data_dir}/acme/account.json`, `DWDAT2`
  store (registered alongside `servers.json`/`inference/llms.json`).
- **Order state** → `{data_dir}/acme/orders/<id>.json`, `DWDAT2`, 24h TTL.
- **Issued cert + key** → `{data_dir}/tls/acme/<name>/{fullchain,privkey}.pem`
  — the *existing* auto-cert location (`server.tls_cert`/`tls_key` already
  accept any path here); plain PEM, not `DWDAT2` (must be readable at
  TLS-listener boot, before `--secure` key derivation).
- `tls_auto_generate`'s self-signed fallback becomes redundant once
  `acme.enabled=true` — PWA surfaces "ACME active; self-signed generator
  disabled" to prevent two writers to `{data_dir}/tls/`.

**Alert events** (per PRD doc 05, §3.5): order initiated (info), challenge
validation failure (warning), order failed (error), **cert expiry within
renewal_days** (error, repeated daily — the specific "quiet miss = outage"
failure mode), account key read failure (critical), all through the
existing alert machinery (no new transport).

## Phases

### Phase 1 (this plan's full scope) — HTTP-01 + renewal + 7 surfaces
- `acme:` config block + validation.
- `lego` HTTP-01 client wired to the daemon's existing HTTP mux (no second
  listener — `lego`'s `Challenge` handler wraps into it).
- Account-key `DWDAT2` store, order-state store.
- Renewer goroutine (6h tick, `renewal_days` threshold, ARI-aware with
  `renewal_days` fallback).
- Apply path: write PEM → point `server.tls_cert`/`tls_key` (+
  `mcp.tls_cert`/`tls_key` if `apply.update_mcp_cert`) → trigger
  `server.auto_restart_on_config`.
- `AlertSink` wired to existing alert machinery.
- `acme.verify` pre-flight (name resolution, port-80 reachability check,
  staging-endpoint reachability) before the first order.
- Seven surfaces: REST `/api/acme/status|renew|verify`; MCP
  `acme_status`/`acme_renew`/`acme_issuer_log`; CLI `datawatch acme
  status|renew|verify`; comm `!acme status|renew|verify`; PWA Settings →
  TLS/Auto-renewal card (cert per domain, expiry countdown, last renewal,
  in-flight order state, renew-now + verify buttons, alert-center tie-in);
  audit/alert rows.

### Out of scope for this plan (deferred, not declined — future work)
- **Phase 2**: DNS-01 (`acme.method: dns01`, B1 delegated-zone via
  `dns_channel` `open: true` mode, B2 scoped provider token). Revisit if a
  wildcard requirement or a port-80-blocked deployment appears.
- **Phase 3**: hot-swap TLS apply (remove `tls_cert`/`tls_key` from
  `RESTART_FIELDS`, mtime-based PEM reload). Revisit once phase 1 is proven
  in production use.
- Plan A (external Caddy) and Plan B (DNS-01 standalone, outside Plan C) —
  declined per decision #1.

## Parity surface

Per the Mobile-Parity Rule and the Configuration Accessibility Rule, all 7
surfaces land together (decision #4): `REST`, `MCP`, `CLI`, `comm channel`,
`YAML/config`, `PWA`. **Android/iOS**: TLS/ACME is a server-operational
concern, not a client-rendered feature surface — the apps consume whatever
cert the daemon serves (same as today's self-signed/manual-cert flow) and
need no app-side change. No `datawatch-app` issue required for this plan;
noted here per the rule's own text ("No issue is needed when the PWA change
is invisible to the operator AND does not change any API contract" — this
*does* add a new PWA Settings card, so: file a `datawatch-app` issue for the
Android/iOS "TLS / Auto-renewal" status card as a fast-follow once the PWA
card ships, matching the project's existing parity cadence — tracked as a
TODO in this doc, not blocking Phase 1).

## Files (representative)

- `internal/acme/` (new package) — `Manager`, `Account`, `OrderState`,
  `Renewer`, `Apply`, `AlertSink`.
- `internal/config/config.go` — new `AcmeConfig` struct + `acme:` YAML key.
- `internal/server/api.go` / `internal/server/acme.go` (new) — REST handlers.
- `internal/mcp/` — `acme_status`/`acme_renew`/`acme_issuer_log` tool
  registration.
- `cmd/datawatch/main.go` — CLI `acme status|renew|verify` subcommand; wire
  `acme.Manager` into daemon startup/shutdown lifecycle.
- comm-channel router — `!acme` verb handling.
- `internal/server/web/app.js` + `locales/*.json` — PWA Settings card.
- `go.mod`/`go.sum` — `github.com/go-acme/lego/v4` dependency.
- `docs/operations.md` — new `acme:` config section; `docs/config-reference.yaml`.
- `docs/testing-tracker.md` — new REST surface entry (B1 rule).

## Verification

- Unit tests: config validation, `Apply`'s PEM-write + atomic rename,
  `AlertSink` event shapes, order-state TTL purge.
- **Live test against `spaceportsouth.dmzs.com` / 66.228.59.180** (not
  simulated) — per this project's own standard of live-verifying
  security/capability surfaces:
  1. Pre-flight: re-confirm port 80/443 free and coordinate with
     `datawatch-app` session's Apple sandbox use immediately before testing.
  2. `acme.enabled=true`, `endpoint=staging`, `domains=[spaceportsouth.dmzs.com]`
     — full issue, confirm PEM written, confirm `server.tls_cert` applied
     after the auto-restart, confirm the staging cert serves (untrusted,
     but structurally correct — verify with `openssl s_client`).
  3. Force-renew via `acme.renew` REST/CLI/MCP — confirm re-apply + restart
     cycle.
  4. Flip to `endpoint=production` — confirm a real browser-trusted cert
     (verify via `curl -v` showing a valid chain, no `-k` needed).
  5. All 7 surfaces individually exercised (REST, MCP, CLI, comm verb,
     PWA card render + renew-now button, alert fires in alert center).
  6. Failure-path spot check: stop the HTTP-01 challenge (block port 80
     temporarily) → confirm `AlertSink` fires the warning/error path, not a
     silent failure.
- `docs/testing-tracker.md` entry for the new REST/MCP/CLI/comm surfaces.
- Per AGENT.md B17: dependency-audit note for `lego` in CHANGELOG.

## Handoff (last step, after verification passes)

The `66.228.59.180` VM is the `datawatch-app` session's Apple App Store
sandbox for iOS TestFlight/release testing (decision log item 6) — this
build is a guest on that box, not the owner. Once Phase 1 is fully built,
live-verified against `spaceportsouth.dmzs.com` (staging → production), and
shipped: **send a `SendMessage` to the `datawatch-app` session** with the
exact version to install/restart to and confirmation the VM is free again
for its Apple sandbox use — do not leave it occupied or in a half-tested
state. This is the explicit close-out of the shared-resource constraint
noted in decision #6, not optional cleanup.

## Resolved (operator interview, continued, 2026-10-06)

7. **Renewal/retry defaults**: `renewal_days: 30` (corrected from the PRD's
   proposed 66 — see config schema comment above), `retry.interval_minutes:
   30`, `retry.max_consecutive_failures: 5`.
8. **MCP cert sharing**: `apply.update_mcp_cert: true` stays the default.
   MCP SSE (`mcp.sse_host`/`mcp.sse_port`) binds to the same public host as
   the main server, just a different port — a TLS cert is host-scoped (SNI/
   SAN), not port-scoped, same reasoning as the sandbox port 8444
   auto-covering (§4.2 of the PRD's Plan C doc). Only relevant when
   `mcp.sse_host` is set to `0.0.0.0` for remote access; loopback-only MCP
   has no TLS surface to share with in the first place.
9. **PWA placement**: no standalone "TLS / Auto-renewal" card. Instead, the
   existing Settings → Web Server card's TLS section (`tls_cert`/`tls_key`/
   `tls_auto_generate`) gains a **certificate-source selector**: `Self-signed
   | Custom cert | Let's Encrypt (ACME)`. Selecting ACME reveals the
   `acme:` fields inline (domains, endpoint staging/production, renewal
   status + countdown, last renewal, in-flight order state, "renew now" +
   "verify" buttons) in place of the manual cert-path fields. This replaces
   the REST/MCP/CLI/comm `/api/acme/*` surfaces' *presentation* only — the
   endpoints themselves are unchanged from §Phase 1 above.
