---
docs:
  index: true
  topics: [acme, letsencrypt, tls, certificates]
exec_params: []
exec_steps:
  - tool: acme_status
    description: Show cert state for every configured ACME domain
    args: {}
    read_only: true
  - tool: acme_issuer_log
    description: DNS resolution + ACME directory reachability pre-flight check
    args: {}
    read_only: true
  - tool: acme_renew
    description: Force an ACME re-order for every configured domain now
    args: {}
    read_only: false
---
# How-to: Native ACME / Let's Encrypt Certificates (BL397)

datawatch can obtain and auto-renew a real, browser-trusted TLS
certificate for a public deployment itself — no external tool (Caddy,
certbot), no DNS provider credential, no zone delegation. The daemon
performs the full ACME HTTP-01 handshake, writes the issued cert
alongside the existing self-signed auto-cert location, and renews it
automatically before it expires.

## What it is

- **Two validation methods.** HTTP-01 (default) — one A/AAAA record per
  hostname, port 80 reachable, no DNS management. DNS-01 — a zone-scoped
  provider API token (Cloudflare in this build), no port 80 needed, the
  only method that supports wildcard domains. Pick HTTP-01 unless you
  need a wildcard or genuinely can't open port 80.
- **Staging first.** `acme.endpoint` defaults to `staging` — Let's
  Encrypt's test directory, generous rate limits, but the issued cert
  isn't trusted by real browsers. Verify the whole flow works there,
  then flip to `production` for a real cert.
- **Apply mode.** By default, a successful issue or renewal writes the
  PEM, updates `server.tls_cert`/`tls_key`, and restarts the daemon so
  the TLS listener picks it up — one short window roughly every 90 days.
  Set `acme.apply.hot_swap: true` for zero-downtime renewals instead —
  the listener reloads the cert from disk on its next handshake, no
  restart (the first-ever switch to ACME still restarts once regardless,
  since the running listener isn't watching the new path yet).
- **Renews itself.** A background check runs on startup and every 6
  hours; a cert within `renewal_days` (default 30) of expiring is
  automatically re-ordered.

## Base requirements

- `datawatch` daemon running and reachable at its public hostname.
- A public DNS A/AAAA record for that hostname pointing at this host.
  **datawatch does not manage DNS** — create this record yourself before
  enabling ACME.
- **Inbound TCP/80 reachable from the public internet.** Let's Encrypt's
  validator connects to port 80 specifically — this is *not* the same as
  `server.port` (default `8080`; the existing dual-port model only
  redirects 8080→8443, it doesn't bind port 80). You need either:
  - `server.port: 80` directly, which on Linux requires root or
    `setcap cap_net_bind_service=+ep <path-to-datawatch-binary>` if the
    daemon runs as an unprivileged user, or
  - an external port-forward from 80 to whatever `server.port` is.
- Firewall: open TCP/80 inbound (`ufw allow 80/tcp` or equivalent).

## Enable it

1. Confirm the DNS record resolves:
   ```bash
   dig +short your-hostname.example.com A
   ```
2. Edit `config.yaml` (or use the PWA Settings → Web Server card's
   certificate-source selector — see below):
   ```yaml
   server:
     port: 80   # see "Base requirements" above
   acme:
     enabled: true
     endpoint: staging
     domains:
       - your-hostname.example.com
   ```
3. Restart the daemon. Watch the issuer log:
   ```bash
   datawatch acme status
   ```
   `issued: true` with a `not_after` date roughly 90 days out means it
   worked. `last_error` set and `consecutive_failures` climbing means
   something's wrong — check `acme verify` next.
4. Verify the pre-flight conditions directly:
   ```bash
   datawatch acme verify
   ```
   Reports whether every configured domain resolves and whether the
   configured ACME directory is reachable. Does not perform an order —
   safe to run anytime.
5. Once staging issues cleanly, flip to production:
   ```yaml
   acme:
     endpoint: production
   ```
   then force a renewal so it actually re-orders against the new
   directory (switching the config alone doesn't trigger a new order
   until the cert is actually due for renewal):
   ```bash
   datawatch acme renew
   ```
6. Confirm from outside with a real browser or `curl` **without** any
   `-k`/insecure flag — a clean connection with no certificate warning is
   the real acceptance test:
   ```bash
   curl https://your-hostname.example.com:8443/api/health
   ```

## Using DNS-01 instead (wildcards, or no port 80)

1. Mint a **zone-scoped** API token — Cloudflare "Zone > DNS > Edit" on
   the ONE zone you're issuing for, never the account-global key.
2. Store it via the secrets manager:
   ```bash
   datawatch secret set cf-zone-edit-token "<your-token>"
   ```
3. Config:
   ```yaml
   acme:
     method: dns01
     dns01:
       provider: cloudflare
       token_secret: "${secret:cf-zone-edit-token}"
   ```
4. Restart — no port 80 / firewall changes needed for this path. The
   rest of the workflow (staging first, `acme verify`, flip to
   production) is identical to HTTP-01.

## PWA

Settings → Web Server card's TLS section now has a certificate-source
selector: **Self-signed** (the original auto-generate behavior) |
**Custom cert path** | **Let's Encrypt**. Selecting Let's Encrypt reveals
the domains/endpoint fields plus a live status readout and Renew Now /
Verify buttons — same underlying REST calls as the CLI above.

## Other surfaces

| Surface | Form |
|---|---|
| REST | `GET /api/acme/status`, `POST /api/acme/renew`, `GET /api/acme/verify` |
| MCP | `acme_status`, `acme_renew`, `acme_issuer_log` |
| CLI | `datawatch acme status\|renew\|verify` |
| Comm | `!acme status\|renew\|verify` |
| Config | `acme:` block — see [config-reference.yaml](../config-reference.yaml) |

## Troubleshooting

- **`acme_status` shows `enabled: false` even though `acme.enabled: true`
  is set** — the subsystem failed to start at boot (check daemon logs for
  `[acme] startup failed`). Common cause: the account couldn't register
  (network blocked outbound 443, or a stale registration from switching
  staging↔production — the daemon re-registers automatically on an
  endpoint change, so this should self-heal on the next restart).
- **Order fails, `last_error` mentions a timeout or connection refused**
  — port 80 isn't actually reachable from the public internet. Check the
  firewall and that `server.port` is really `80` (not `8080`).
- **Switched to `production` but still see the old self-signed cert** —
  switching `acme.endpoint` doesn't force a new order by itself (the
  existing cert isn't due for renewal yet); run `datawatch acme renew`
  explicitly.

## See also

- [docs/plans/historical-plans/2026-10-06-bl397-native-acme-letsencrypt.md](../plans/historical-plans/2026-10-06-bl397-native-acme-letsencrypt.md)
  — full design, the operator-interview decision log, and four real bugs
  found and fixed during live verification against a real Let's Encrypt
  directory.
- [operations.md](../operations.md) "Native ACME / Let's Encrypt" section.
