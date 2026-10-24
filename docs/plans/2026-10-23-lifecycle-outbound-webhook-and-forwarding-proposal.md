# Lifecycle outbound webhook sink and federation forwarding — proposal

Status: PROPOSAL - awaiting operator confirmation. The sink and the forwarding ship disabled until this is confirmed.

Part of BL427 Phase 5. Parent design: [2026-10-13-prd-lifecycle-events.md](2026-10-13-prd-lifecycle-events.md)
(Q2, Phases item 5, Follow-up implementation, Parity surface, open questions 5 and 7).

Fits the code as it is today: `internal/autonomous/lifecycle_subscription.go` (`webhook` is a reserved
sink type that `ValidateSubscription` rejects with a "later phase" error), `lifecycle_session_push.go`
(the engine's `handle` switch and the `SetAuditFn` audit path), `lifecycle_event.go` (`LifecycleEvent`
already has `Host`), `internal/server/push_ssrf.go` (loopback / link-local / metadata classification and
the dial-time `net.Dialer.Control` re-check), `internal/federation/hopchain.go` (HMAC-SHA256 over the
shared peer token), `internal/server/multiserver/store.go` (`Entry.Federated`, `Capabilities`,
`HTTPClient`), `internal/federation/capabilities.go` (`autonomous:write`) and `internal/secrets/refs.go`
(`${secret:name}`, `IsRef`, `ResolveRef`).

## 1. Already decided by the operator (2026-10-23)

- Outbound webhook sink: **yes**, with an operator-configured host allow-list (only listed hosts can
  receive events), the signing secret from the secrets vault, each POST signed with an HMAC, failures
  retried with backoff and then an alert raised, and an SSRF guard that refuses private, loopback and
  link-local targets unless the host is explicitly allow-listed.
- Federation forwarding: **yes**, through the existing federation path (the peer registry, the peer's
  bearer token and the hop-chain header).
- The existing generic webhook messaging backend is inbound-only and **is not changed**. The new sink
  is separate code in `internal/autonomous`.

## 2. Needs your confirmation

Every value below is a recommendation. Each one lives in one named constant or config default marked
"pending operator confirmation", so a changed answer is a one-place edit.

### (a) Retry count and backoff curve

- **RECOMMENDED:** 5 attempts in total. Delays between attempts 1s, 4s, 16s, 64s, each with ±20% random
  jitter. Per-attempt timeout 10s (connect + request + response). Worst case about 2.5 minutes for one
  event. All four numbers configurable (section 4).
- Retried: network error, timeout, `408`, `429` (a `Retry-After` is honoured when it is longer than the
  next delay, capped at 120s), and any `5xx`.
- Never followed: `3xx` (a redirect counts as a permanent failure, see section 3).
- Fail immediately, no retry: every other `4xx`, an unresolvable `${secret:...}` reference, a payload
  that is over the size cap, a target refused by the guard.
- After the last failed attempt: an alert is raised (alert store, warning level, naming the subscription,
  the host and the final status) and an audit entry is written.
- Alternatives: (1) 3 attempts, 2s / 10s: less held-up queue, more events lost to a short outage;
  (2) 8 attempts over about 10 minutes: survives longer outages but holds the subscription's queue and a
  goroutine for that long, and still loses events on a daemon restart.
- Reason: retries run on the subscription's own dispatcher goroutine (as the session sink already does),
  so a stalled receiver delays only that subscription. 2.5 minutes covers a receiver restart without
  letting the 256-slot queue fill in normal use.

### (b) Signature header name and format

- **RECOMMENDED:** timestamped HMAC-SHA256 carried in one header, plus two identification headers.

  | Header | Value |
  |---|---|
  | `X-Datawatch-Signature` | `t=<unix-seconds>,v1=<lowercase hex>` |
  | `X-Datawatch-Delivery` | the delivery id (32 hex chars), identical on every retry of one delivery |
  | `X-Datawatch-Event` | the event kind, e.g. `prd.completed` or `task.session_started` |

  Also sent: `Content-Type: application/json` and `User-Agent: datawatch-lifecycle/1`.
- Signed value: `v1 = hex( HMAC-SHA256( secret, "<t>" + "." + <raw request body bytes> ) )`.
  Each attempt is signed afresh with the current time; the body bytes do not change between attempts.
- **Receiver recipe:** (1) read the raw body before any JSON parsing; (2) split the header on `,` and
  read `t` and `v1`; (3) reject if `abs(now - t) > 300` seconds (replay tolerance, recommended 5
  minutes); (4) compute the HMAC over `t + "." + rawBody` with the shared secret; (5) compare with a
  constant-time comparison (`hmac.Equal` and the like); (6) accept only on a match, and de-duplicate on
  `X-Datawatch-Delivery`, because a retry after a lost response arrives twice.
- **Secret reference a subscription carries:** `sink.secret` holds exactly one vault reference such as
  `${secret:lifecycle-hook-prod}`. A literal value is refused at creation (checked with
  `secrets.IsRef`); an unsigned sink is not allowed. The reference is resolved on every delivery, so a
  rotation takes effect on the next event, and the resolved value is never stored in the subscription,
  logged, audited or returned by any surface. Subscription example:
  `{"sink":{"type":"webhook","target":"https://hooks.example.net/datawatch","secret":"${secret:lifecycle-hook-prod}"}}`.
  This adds one optional `secret` field to `SubscriptionSink` (webhook only; refused on other types).
- Alternatives: (1) GitHub style `X-Hub-Signature-256: sha256=<hex>` over the body only: the widest
  receiver support, but no replay protection; (2) the same HMAC with the timestamp in a separate
  `X-Datawatch-Timestamp` header: slightly easier to read, three headers to keep consistent.
- Reason: one header that binds the timestamp into the signed bytes cannot be half-replayed, and the
  format matches what receivers of other signed webhooks already implement.

### (c) Event payload shape and size cap

- **RECOMMENDED:** a versioned JSON envelope around the existing event JSON, unchanged.

  | Field | Meaning |
  |---|---|
  | `version` | envelope version, integer, `1` |
  | `delivery_id` | same value as `X-Datawatch-Delivery` |
  | `sent_at` | RFC 3339 UTC time of the first attempt (same on every retry) |
  | `host` | the sending daemon's host name |
  | `subscription_id` | the subscription that matched |
  | `event` | the `LifecycleEvent` exactly as the other sinks see it (id, at, kind, level, the Automaton and task ids, old and new status, session id, decision, verdict, host, test flag) |
  | `truncated` | `true` only when `decision` and `verdict` were removed to fit the cap; omitted otherwise |
  | `forward` | present only on a peer-forwarded envelope: `origin_host`, `hops` (integer), `visited` (list of host names), see (d) and section 3 |

- **Body size cap:** 64 KiB (65536 bytes) of the serialised envelope. Over the cap: drop `event.decision`
  and `event.verdict`, set `truncated`, serialise again; still over: the delivery fails without being
  sent (permanent, audited, alert raised). Configurable.
- Alternatives: (1) the bare event with the metadata in headers: smaller, but the delivery id, host and
  forward data then cannot be signed with the body; (2) a cap of 256 KiB or 16 KiB.
- Reason: the envelope version lets the shape grow without breaking receivers, and everything a receiver
  needs to de-duplicate and attribute is inside the signed bytes.

### (d) How a subscription addresses a peer

- **RECOMMENDED:** a new sink type `peer` (the name `federation` is the alternative). `sink.target` is the
  registered peer name from the multi-server registry (`Entry.Name`). No URL, no secret field: the peer's
  address, TLS-skip setting and bearer token come from the registry, through `multiserver.HTTPClient`.
- **Validation (at creation and again at each delivery):** the entry exists, is `Enabled`, has
  `Federated` set, and its capabilities resolve (`federation.Resolve`) to include `autonomous:write`.
  Anything else is refused with a message that names which condition failed.
- **Transport:** `POST <peer url>/api/autonomous/events/forward` with the same envelope, signature
  headers (b) (the peer's bearer token is the HMAC key, as in the hop chain), the bearer token for
  authentication and an `X-Datawatch-Hop-Chain` header extended with `federation.BuildOrExtendChain`
  so the receiver can attribute the call to the origin daemon.
- **Receiving daemon:** verifies the bearer token and `autonomous:write` for the presenting peer, checks
  the signature and the loop rules (section 3), then re-emits the event into its own local dispatcher
  with the forwarded marker and the origin host set. Local subscriptions of any sink type (comm,
  session, topic, alert, mcp_queue, webhook) then see it like any other event; the marker shows as
  `forward.origin_host` and is added to the text a human sink prints. The receiver never forwards it on.
- Alternatives: (1) reuse `webhook` with the peer's URL typed in: needs the token copied into the
  subscription and bypasses the registry; (2) forward every event to every federated peer by a global
  switch: no per-subscription filtering, noisy.
- Reason: the registry already is the operator's list of trusted peers and holds the token, so a
  subscription names a peer and never carries a credential.

### (e) Peer offline

- **RECOMMENDED:** the same bounded retry as (a), then drop with an alert and an audit entry. No durable
  outbox. Forwarding is best-effort; the sending daemon's own Automaton state is the record.
- Alternative: **durable outbox**: persist each undelivered envelope per peer (encrypted with `secfile`
  like the other stores), replay in order when the peer returns, with a retention cap and an expiry.
  Cost: a new persisted store and replay loop, ordering and duplicate rules after a restart, an expiry
  policy so a week-old `task.session_started` is not replayed into a live session, disk growth during a
  long outage, and one more thing to inspect in every surface. About the size of the sink itself.
- Alternative: a longer in-memory retry (as in (a) alternative 2): cheap, still lost on restart.
- Reason: lifecycle events are notifications about state the peer can read from the API; an outbox is a
  delivery guarantee nobody has asked for yet. It can be added later without changing the wire format.

## 3. Also decided in this proposal, please confirm

- **Loop prevention.** The forwarded envelope carries `forward.origin_host`, `forward.hops` and
  `forward.visited`. A daemon never forwards an event it received as forwarded (its `peer` sinks skip any
  event marked forwarded). A receiver refuses an envelope whose `visited` already lists the local host
  name, or whose `hops` exceeds the hop cap (**recommended 3**, one named constant), or whose `origin_host`
  equals the local host. Refusals answer `409`, are audited, and are not retried by the sender.
- **URL scheme.** `https` only. `http` is accepted only for a host that is explicitly in the allow-list.
  Any other scheme, a URL with userinfo (`user:pass@`) or a URL with no host is refused at creation.
- **Allow-list match.** Exact `host` or exact `host:port`, case-insensitive, compared after the URL is
  parsed; no wildcards and no suffix match; a bare `host` entry matches that host on any port, a
  `host:port` entry only that port. An empty list means nothing can receive an event; every `webhook`
  subscription is refused while the list is empty. The check runs at creation and at every delivery.
- **SSRF guard.** Loopback, link-local, unspecified and private (RFC 1918, ULA, Tailscale CGNAT)
  addresses are refused **unless the host is explicitly allow-listed**. This reuses the classification
  functions in `push_ssrf.go` (moved to a shared package so `internal/autonomous` does not import
  `internal/server`).
- **Cloud metadata address (ASK EXPLICITLY).** **Recommended: stays refused even when its host is
  allow-listed** (`169.254.169.254`, `fd00:ec2::254`). This is a deliberate narrowing of the operator
  rule "unless explicitly allow-listed". Reason: no legitimate event receiver lives there and a typo in
  an allow-list entry must not turn the daemon into a credential fetcher. Alternative: honour the
  allow-list for every address. **Do you accept the narrowing?**
- **DNS rebinding.** The resolved IP is checked again at dial time with a `net.Dialer.Control` hook (the
  `push_ssrf.go` pattern), on every attempt, so a host that resolved safely at creation and later points
  at a blocked address is refused. The connection goes to the checked IP, not to a second lookup.
- **Redirects.** Never followed (`CheckRedirect` returns `http.ErrUseLastResponse`); a `3xx` is a
  permanent failure.
- **Audit entry for every delivery attempt** (JSON-lines and CEF through the `SetAuditFn` path, action
  `autonomous_lifecycle_delivery`): `subscription_id`, `owner`, `delivery_id`, `sink` (`webhook` or
  `peer`), `target_host` (host and port only), `event_kind`, the Automaton id and task id of the event,
  `attempt` and `attempts_max`, `outcome` (`delivered`, `retry`, `failed`, `refused`), `status_code`,
  `error_class` (`timeout`, `network`, `http_4xx`, `http_5xx`, `guard`, `secret`, `size`, `loop`),
  `duration_ms`. **Never** the secret or its resolved value, the signature, the request body, URL
  userinfo or the URL query string; the path is recorded only after stripping the query.

## 4. What ships disabled

Both features are inert until `enabled` is set to `true`. While `enabled` is `false`: a `webhook` or
`peer` subscription is refused at creation with a message naming the key to set, and any such
subscription already stored delivers nothing. Keys (documented in `docs/config-reference.yaml`, same
change as the code):

| Key | Default | Meaning |
|---|---|---|
| `autonomous.notify.webhook.enabled` | `false` | master switch for the webhook sink |
| `autonomous.notify.webhook.allow_hosts` | `[]` | exact `host` or `host:port` entries; empty means nothing can receive |
| `autonomous.notify.webhook.max_attempts` | `5` | (a) total attempts |
| `autonomous.notify.webhook.backoff_seconds` | `[1, 4, 16, 64]` | (a) delays between attempts |
| `autonomous.notify.webhook.jitter_percent` | `20` | (a) ± spread on each delay |
| `autonomous.notify.webhook.attempt_timeout_seconds` | `10` | (a) per-attempt timeout |
| `autonomous.notify.webhook.max_body_bytes` | `65536` | (c) envelope cap |
| `autonomous.notify.webhook.signature_tolerance_seconds` | `300` | (b) replay tolerance printed in the receiver recipe |
| `autonomous.notify.forwarding.enabled` | `false` | master switch for the `peer` sink and the receive endpoint |
| `autonomous.notify.forwarding.max_hops` | `3` | section 3 hop cap |
| `autonomous.notify.forwarding.max_attempts`, `.backoff_seconds`, `.jitter_percent`, `.attempt_timeout_seconds` | as the webhook values | (e) bounded retry for peers |

The wire details that are not config values (header names, the signed string format, the envelope
version and field names, the sink type name `peer`, the receive path) are named constants in one file,
each commented "pending operator confirmation". The implementation reads every open value from that one
place, so a changed decision is a one-line edit plus its test expectation.

## 5. Confirmation checklist

Reply with the box ticked, or with the changed value.

- [ ] (a) 5 attempts, delays 1s / 4s / 16s / 64s ±20% jitter, 10s per-attempt timeout; retry on network error, timeout, 408, 429, 5xx; 3xx never followed; other 4xx fail immediately
- [ ] (b) `X-Datawatch-Signature: t=<ts>,v1=<hex>` = HMAC-SHA256 over `<ts>.<raw body>`, plus `X-Datawatch-Delivery` and `X-Datawatch-Event`; 300s replay tolerance; `sink.secret` is one `${secret:name}` reference
- [ ] (c) versioned envelope (`version`, `delivery_id`, `sent_at`, `host`, `subscription_id`, `event`, `truncated`, `forward`), 64 KiB cap
- [ ] (d) sink type `peer`, target is the registered peer name, federated + enabled + `autonomous:write`, receiver re-emits locally marked as forwarded
- [ ] (e) offline peer: bounded retry, then drop with alert and audit entry, no durable outbox
- [ ] Loop prevention: origin host + hop count + visited list, never forward a forwarded event, hop cap 3
- [ ] URL scheme: `https` only, `http` only for an explicitly allow-listed host
- [ ] Allow-list: exact host or host:port, case-insensitive, no wildcards, empty list means nothing can receive
- [ ] Cloud metadata address refused even when allow-listed (narrowing of the operator rule — answer explicitly)
- [ ] DNS rebinding defence: re-check the resolved IP at dial time on every attempt
- [ ] Redirects are not followed
- [ ] Audit fields as listed, never the secret, URL userinfo or query string
