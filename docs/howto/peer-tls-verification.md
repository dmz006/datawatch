---
docs:
  index: true
  topics: [federation, peers, security, tls, workers, observer]
exec_steps:
  - tool: federation_peer_get
    description: Read one peer entry — check its current tls_cert_fingerprint / tls_skip_verify
    args:
      name: "{{params.peer_name}}"
    read_only: true
  - tool: federation_peer_update
    description: Pin the peer's leaf certificate (64-hex SHA-256). Rejected when both are set with tls_skip_verify
    args:
      name: "{{params.peer_name}}"
      tls_cert_fingerprint: "{{params.fingerprint}}"
    read_only: false
  - tool: federation_peer_test
    description: Probe the peer's health endpoint — confirms the pin (or opt-out) actually works
    args:
      name: "{{params.peer_name}}"
    read_only: true
---

# Peer TLS: verification and the certificate pin

Every datawatch link that crosses `https://` now checks the other side's
TLS certificate by default — against the **system trust store** — and
offers two ways to handle a peer that presents a self-signed or
private-CA certificate instead of a browser-trusted one:

- **Pin the leaf certificate** (recommended): `tls_cert_fingerprint`, a
  64-hex SHA-256 fingerprint of the peer's *leaf* certificate. The chain
  is not checked and the leaf must match, so a renewed or re-issued leaf
  breaks the pin until you update it.
- **Skip verification** (opt-out, always announced): for a
  self-signed/local-dev peer on a trusted network only.

## The three links, at a glance

| Link | Default trust | Pin | Opt-out |
|------|---------------|-----|---------|
| Federation peers / remote servers (outbound API calls) | system trust store | `tls_cert_fingerprint` on the peer entry | `tls_skip_verify: true` on the peer entry |
| Worker observer push → parent (`https://` parent) | system trust store (one startup notice) | `DATAWATCH_PARENT_CERT_FINGERPRINT` | `DATAWATCH_OBSERVER_PUSH_INSECURE=1` |
| Federation observer, primary → root | system trust store | trust the root's certificate | `observer.federation.insecure: true` |

An `http://` endpoint has no certificate to check; none of the settings
apply to it.

The **pin always wins over the opt-out**: a worker push with both
`DATAWATCH_PARENT_CERT_FINGERPRINT` and the insecure opt-out set still
verifies against the pin, and a peer entry cannot carry both
`tls_cert_fingerprint` and `tls_skip_verify` — the request is rejected.

## Deriving a pin

From the leaf certificate file:

```bash
openssl x509 -noout -fingerprint -sha256 -in leaf.pem
# sha256 Fingerprint=AB:CD:...   (paste the 64 hex digits, colons optional)
```

Or straight off a live peer:

```bash
openssl s_client -connect peer.example.net:8443 </dev/null 2>/dev/null \
  | openssl x509 -noout -fingerprint -sha256
```

`s_client` prints the whole chain; the first certificate block is the
**leaf** — that is the one whose fingerprint you pin. Colons, spaces and
case are all accepted; the value is stored as 64 lowercase hex digits.
A malformed pin is **rejected when saved** (REST `400`, CLI error) and,
on the worker push path, makes the push refuse to start (fail closed).

## Pinning a peer entry

Set `tls_cert_fingerprint` on the peer in whichever surface you manage
peers from — they all write the same field:

- **REST**: `tls_cert_fingerprint` in the `POST`/`PUT` body on
  `/api/servers` (and `/api/federation/peers`); a malformed pin or a pin
  combined with `tls_skip_verify` is a `400`.
- **MCP**: `tls_cert_fingerprint` on `federation_peer_add` /
  `federation_peer_update`.
- **CLI**: `datawatch federation peer add|update --tls-cert-fingerprint
  <64-hex>` (refused together with `--tls-skip-verify`).
- **YAML**: `tls_cert_fingerprint: "<64-hex>"` on the `servers:` entry
  (daemon restart required).
- **PWA**: the **Certificate fingerprint (pin)** field in the server
  Add/Edit form.

Read it back with `federation_peer_get`, `GET /api/servers`, or the PWA
form, and confirm the link with `federation_peer_test` (or
`POST /api/servers/{name}/test`).

Because a pin is one leaf certificate, **renewing the peer's
certificate invalidates the pin** — every peer goes unreachable until
you save the new fingerprint. That is the trade for not trusting the
whole CA.

## Skipping verification (the warned opt-out)

- **Peer entry** — `tls_skip_verify: true` turns certificate checking off
  for *that peer only*; every other peer keeps full verification. Saving
  such a peer is announced three ways: the create/update response carries
  a `warnings` array (also on `POST /api/servers/{name}/test` and the
  federation `test` endpoint), the daemon logs
  `<name>: this peer's certificate is not verified (tls_skip_verify is
  set); set tls_cert_fingerprint to pin its leaf certificate instead`,
  and it emits a warn-level system alert titled **Peer certificate not
  verified** — at most once per peer per daemon run (a startup sweep also
  covers peers that were already saved, so they warn exactly once). The
  alert is the review queue: fix each one with a pin or a trusted CA.
- **Worker observer push** — `DATAWATCH_OBSERVER_PUSH_INSECURE`
  (`1|true|yes|on`, any case) skips verification on the push client and
  prints a startup warning naming the parent host —
  `observer peer: TLS certificate verification is SKIPPED for parent
  <host>. Anything on the network path can read and alter the pushed
  stats. ...` — never any token. An `https://` parent with neither a pin
  nor the opt-out prints the one-line notice that verification uses the
  system trust store and that a self-signed parent needs the pin or the
  opt-out.
- **Federation observer, primary → root** — `observer.federation.insecure:
  true` still skips verification, but the daemon prints `WARNING: TLS
  certificate verification is SKIPPED for the federation root <url>` at
  startup. This push has no pin knob; trust the root's certificate
  instead.

Unchanged by any of this: the bootstrap channel's existing fallback for a
TLS-less parent, and `datawatch-stats`, the separate reporter binary with
its own `--insecure-tls` flag.

## Upgrading a fleet: what can break

Links that used to work only because verification was off on one side
will fail the TLS handshake after an upgrade until you do one of three
things — pin the peer's leaf, trust its CA (system trust store or CA
file where the surface offers one), or set the explicit opt-out:

- **Peers you reached with a self-signed or private-CA certificate** are
  no longer reachable until pinned or skipped. If you trusted the CA in
  the browser only, note that the daemon's own calls need the CA in the
  *daemon's* system trust store.
- **Workers pushing to a self-signed `https://` parent** — the push used
  to skip certificate checking unconditionally; now it verifies. Pin
  `DATAWATCH_PARENT_CERT_FINGERPRINT` (the parent already injects this
  for the bootstrap channel, so the value usually travels with the
  worker) or set the opt-out with its warning.
- **A primary pushing into a self-signed federation root** needs the
  root's certificate in the system trust store, or the (warned)
  `observer.federation.insecure: true`.

## Related

- [howto/multi-servers → Peer TLS (verified by default)](multi-servers.md#peer-tls-verified-by-default) — the full per-surface walkthrough
- [howto/federated-observer → Push-channel TLS (verified by default)](federated-observer.md#push-channel-tls-verified-by-default) — worker and federation push paths
- [howto/container-workers](container-workers.md) — workers and their parent push
- [datawatch-definitions → Push-channel TLS](../datawatch-definitions.md) — glossary entry
- [encryption → Network transit](../encryption.md) — the at-rest/at-transit picture
- [howto/federation-fingerprint](federation-fingerprint.md) — the per-peer `fingerprint-sync` flag (a different thing from the `tls_cert_fingerprint` pin)
