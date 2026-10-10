# Native ACME / Let's Encrypt flow (BL397)

How `acme.enabled: true` turns into a browser-trusted certificate —
account registration, the HTTP-01 challenge (served through the
daemon's EXISTING mux, no new listener), issuance, and the restart-based
apply that gets the new cert into the live TLS listener.

```
   ┌─── daemon startup (cfg.Acme.Enabled) ─────────────────────────────┐
   │                                                                   │
   │  acme.NewManager(fullCfg, dataDir, cfgPath, encKey, restartFn)    │
   │      │                                                            │
   │      ├─→ LoadOrCreateAccount({data_dir}/acme/account.json)        │
   │      │   DWDAT2-encrypted ECDSA P-256 key (generated once,        │
   │      │   reused forever — this is the account's identity, NOT    │
   │      │   a cert key)                                              │
   │      │                                                            │
   │      ├─→ loadExistingCertStatus(dataDir, domains)                 │
   │      │   reads {data_dir}/tls/acme/<name>/fullchain.pem if        │
   │      │   present — seeds Issued/NotAfter from the REAL cert,      │
   │      │   not a blank "not issued" assumption (restart-state bug,  │
   │      │   see below)                                               │
   │      │                                                            │
   │      └─→ IsRegisteredFor(directoryURL)?                           │
   │          staging and production are SEPARATE registries — a      │
   │          registration valid against one is rejected by the        │
   │          other. If not registered for THIS directory:             │
   │              ClearRegistration()  (nils in-memory reg so lego     │
   │                                    signs new-account with an      │
   │                                    embedded JWK, not a stale      │
   │                                    KeyID)                         │
   │              client.Registration.Register(...)                   │
   │              SetRegistration(reg, directoryURL)  → persisted      │
   │                                                                   │
   └───────────────────────────────┬───────────────────────────────────┘
                                   │ Manager.Start()
                                   ▼
   ┌─── Renewer goroutine ─────────────────────────────────────────────┐
   │                                                                   │
   │  checkAndRenewAll() ── runs ONCE immediately, then every 6h       │
   │      │                                                            │
   │      │  needsRenew = !Issued || remaining-life <= renewal_days    │
   │      ▼                                                            │
   │  IssueNow()  (also reachable directly via REST/MCP/CLI/comm renew)│
   │      │                                                            │
   │      ├─→ client.Certificate.Obtain({Domains: [...]})              │
   │      │   ONE multi-SAN order for every configured domain          │
   │      │       │                                                    │
   │      │       ▼                                                    │
   │      │   ┌─── HTTP-01 challenge round trip ──────────────────┐   │
   │      │   │                                                    │   │
   │      │   │  lego → provider.Present(domain, token, keyAuth)   │   │
   │      │   │      httpProvider stores token→keyAuth in memory   │   │
   │      │   │                                                    │   │
   │      │   │  Let's Encrypt validator ──GET──→ :80               │   │
   │      │   │      /.well-known/acme-challenge/<token>            │   │
   │      │   │                                                    │   │
   │      │   │  redirectToTLSHandler: UNCONDITIONAL bypass for     │   │
   │      │   │  this path prefix (not loopback-gated like every    │   │
   │      │   │  other bypass — LE's validator is a public IP).     │   │
   │      │   │  Without this, the validator gets redirected to     │   │
   │      │   │  the OLD self-signed cert on :8443 and can't trust  │   │
   │      │   │  it — order silently fails.                         │   │
   │      │   │                                                    │   │
   │      │   │  ──→ httpProvider.Handler() serves keyAuth in       │   │
   │      │   │      plaintext, 200 OK                              │   │
   │      │   │                                                    │   │
   │      │   │  Let's Encrypt validates, lego → CleanUp()          │   │
   │      │   └────────────────────────────────────────────────────┘   │
   │      │                                                            │
   │      └─→ certificate.Resource{Certificate, PrivateKey}            │
   │                                                                   │
   │  applyCertificate(res)                                            │
   │      │                                                            │
   │      ├─→ write {data_dir}/tls/acme/<name>/{fullchain,privkey}.pem │
   │      │   (atomic: tmp file + rename)                              │
   │      │                                                            │
   │      ├─→ fullCfg.Server.TLSCert/TLSKey = <new paths>              │
   │      │   fullCfg.MCP.TLSCert/TLSKey = <same>, if UpdateMCPCert    │
   │      │   config.Save(fullCfg, cfgPath)                            │
   │      │   (THIS is what actually makes the restart below pick up  │
   │      │   the new cert — missing entirely in the first live-test  │
   │      │   pass: PEM written + Status() said "issued", but the     │
   │      │   TLS listener kept serving the old self-signed cert)     │
   │      │                                                            │
   │      ├─→ update DomainStatus (Issued, NotAfter, LastRenewal)      │
   │      │   + logEvent → alerts.EmitSystem (existing alert pipe,     │
   │      │     no new transport)                                     │
   │      │                                                            │
   │      └─→ go func() { sleep(500ms); restartFn() }                  │
   │          delayed so an in-flight HTTP response (e.g. the REST     │
   │          caller of POST /api/acme/renew) finishes flushing        │
   │          before the process exits — calling restartFn()           │
   │          synchronously raced the response and the caller saw      │
   │          an empty body despite the renewal succeeding             │
   │                                                                   │
   └───────────────────────────────┬───────────────────────────────────┘
                                   │ daemon restarts
                                   ▼
            TLS listener boots, reads server.tls_cert/tls_key
            from the now-updated config → serves the REAL cert
```

## Failure path (consecutive failures)

```
   IssueNow() returns an error (e.g. port 80 unreachable, DNS wrong)
       │
       ▼
   recordFailure(domain, err)
       │
       ├─→ st.ConsecutiveFailures++
       ├─→ severity = warn, escalating to error at
       │   retry.max_consecutive_failures (default 5)
       └─→ logEvent(severity, "ACME order failed: "+domain, err)
           → alerts.EmitSystem → PWA alert center / comm channels

   Next attempt: the 6h Renewer tick (or an explicit renew call) —
   there is no separate retry-interval timer in this build; a failed
   attempt just waits for the next regular check.
```

## Where this plugs into the rest of the daemon

- **7 surfaces**, all backed by the same `Manager`: REST
  (`/api/acme/{status,renew,verify}`), MCP (`acme_status`/`acme_renew`/
  `acme_issuer_log`), CLI (`datawatch acme ...`), comm (`!acme ...`), PWA
  (Web Server card's certificate-source selector), YAML (`acme:` block),
  audit/alerts (existing `alerts.EmitSystem` pipe — no new transport).
- **No new listener.** `httpProvider.Handler()` is registered on the
  daemon's existing public mux at `http01.PathPrefix`
  (`/.well-known/acme-challenge/`) — the same port that already serves
  `server.port`'s HTTP→HTTPS redirect.
- **`Verify()`** (REST `/api/acme/verify`, CLI `acme verify`, comm `acme
  verify`) is a separate, non-mutating pre-flight check — DNS resolution
  + ACME directory reachability — distinct from the MCP tool
  `acme_issuer_log`, which returns the bounded recent-event history
  instead.

Full design + the operator-interview decision log + live-test bug
writeups: [docs/plans/historical-plans/2026-10-06-bl397-native-acme-letsencrypt.md](../plans/historical-plans/2026-10-06-bl397-native-acme-letsencrypt.md).
Operator workflow: [docs/howto/letsencrypt-acme.md](../howto/letsencrypt-acme.md).
