---
docs:
  index: true
  topics: [autonomous, lifecycle, webhooks, federation, security]
exec_params: []
exec_steps:
  - tool: get_config
    description: Read the autonomous settings, including the webhook and forwarding switches (both ship off)
    args: {section: "autonomous"}
    read_only: true
  - tool: autonomous_subscriptions
    description: List the persisted lifecycle subscriptions, including any webhook and peer rules
    args: {}
    read_only: true
  - tool: federation_peer_list
    description: List the registered federation peers that a peer subscription can name
    args: {}
    read_only: true
  - tool: secret_list
    description: List vault secret names (values are never shown) to find the signing secret reference
    args: {}
    read_only: true
---
# How-to: Automaton lifecycle webhooks and peer forwarding

The lifecycle subscriptions described in
[Automata lifecycle subscriptions](automaton-lifecycle-subscriptions.md)
deliver events to a chat channel, a session, a topic, the alert store or a
polling queue. This guide covers two more sinks that send events **out of
this daemon**:

- **Outbound webhook** — each matching event is an HTTPS `POST` to a
  receiver you run. Every request is signed with HMAC-SHA256 so the
  receiver can prove who sent it and that it was not changed.
- **Peer forwarding** — each matching event is sent to another datawatch
  daemon that you have registered as a federation peer. The receiving
  daemon re-emits it to its own subscriptions.

> **Both features ship disabled.** Until you turn them on, a webhook or peer
> subscription is refused when you create it, and any such subscription that
> is already stored delivers nothing. Subscriptions with the other sink types
> are not affected.
>
> The retry counts, the signature format and the payload shape in this guide
> are the proposed defaults in the
> [lifecycle webhook and forwarding proposal](../plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md).
> They are pending the operator's confirmation. Keep both features off until
> that confirmation is recorded.

## What happens to an event

1. An Automaton emits a lifecycle event (for example `task.completed` or
   `prd.blocked`).
2. The subscription engine checks every stored subscription against it. A
   webhook or peer subscription that matches is handed to its sink.
3. The sink builds one JSON envelope for the event, signs it and sends it.
   A failed send is retried with a growing delay. A send that is still
   failing after the last attempt is dropped, raises one alert, and writes
   an audit record.

Every attempt, and the final outcome, is written to the audit log (see
[Retries, failures and audit records](#retries-failures-and-audit-records)).

## Enable the sink and set the allow-list

A webhook can only reach a host that is on the **allow-list**. The list is
the operator's decision about where events may go, and it is checked when a
subscription is created and again on every delivery.

The allow-list entries are:

- an exact host, such as `hooks.example.net` (matches that host on any port), or
- an exact host and port, such as `hooks.example.net:8443` (matches only that
  port).

Entries are case-insensitive. An entry must not contain a scheme, a path, a
query, a wildcard or a user name. An **empty list means nothing can receive
an event**.

The keys are:

| Key | Default | Meaning |
|---|---|---|
| `autonomous.notify.webhook.enabled` | `false` | Master switch for the webhook sink |
| `autonomous.notify.webhook.allow_hosts` | `[]` | Hosts allowed to receive a webhook |
| `autonomous.notify.forwarding.enabled` | `false` | Master switch for peer forwarding, in both directions |

Set the same values on every surface below. The other retry and limit keys
are listed in the [configuration reference](../config-reference.yaml).

| Surface | How to turn the sink on and set the allow-list |
|---|---|
| YAML | In `config.yaml`, under `autonomous.notify.webhook`, set `enabled: true` and `allow_hosts: ["hooks.example.net", "hooks.example.net:8443"]`. Under `autonomous.notify.forwarding`, set `enabled: true` for peer forwarding. The file is read when the daemon starts. |
| CLI | `datawatch config set autonomous.notify.webhook.enabled true` then `datawatch config set autonomous.notify.webhook.allow_hosts hooks.example.net,hooks.example.net:8443` |
| Web UI | **Settings → Comms → Notifications**: the **Outbound webhook sink: enabled** toggle and the **Webhook allow-list (host or host:port)** field. Peer forwarding has its own toggle in the same card. |
| REST | `curl -sk -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"autonomous.notify.webhook.enabled": true, "autonomous.notify.webhook.allow_hosts": ["hooks.example.net", "hooks.example.net:8443"]}' https://localhost:8443/api/config` |
| Comm channel | `configure autonomous.notify.webhook.enabled=true` then `configure autonomous.notify.webhook.allow_hosts=hooks.example.net,hooks.example.net:8443` |

The Web UI, REST, CLI and comm channel changes take effect on the next event,
with no restart. A value that is refused leaves the stored settings unchanged.

## Store the signing secret in the vault

The signing key is never typed into a subscription. Store it in the secrets
vault and refer to it by name:

```sh
# Generate a random key, then store it. Keep the value; the receiver needs it too.
openssl rand -hex 32
datawatch secrets set lifecycle-hook-prod "<the value you generated>"
```

A webhook subscription then carries the reference `${secret:lifecycle-hook-prod}`.

- The reference must be exactly one `${secret:name}`. A literal value is
  refused when you create the subscription, so the value is never stored in
  the subscription.
- The daemon resolves the reference on **every delivery**. A rotated secret
  applies to the next event, with no restart. The receiver must be given the
  new value at the same time.
- The resolved value is never written to a subscription, a log line, an audit
  record, an alert or any API response.

## Create a webhook subscription

A webhook subscription needs a filter, the sink type `webhook`, the HTTPS URL
as the target, and the secret reference. The examples below watch for three
event kinds.

**MCP** (any MCP host connected to the daemon):

```text
autonomous_subscribe(
  kinds="task.completed,task.failed,prd.blocked",
  sink_type="webhook",
  sink_target="https://hooks.example.net/datawatch",
  secret_ref="${secret:lifecycle-hook-prod}")
```

**CLI:**

```sh
datawatch autonomous subscribe \
  --on task.completed --on task.failed --on prd.blocked \
  --sink webhook --target https://hooks.example.net/datawatch \
  --secret-ref '${secret:lifecycle-hook-prod}'
```

**REST:**

```sh
curl -sk -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{
    "owner": "operator",
    "filter": {"kinds": ["task.completed", "task.failed", "prd.blocked"]},
    "sink": {"type": "webhook",
             "target": "https://hooks.example.net/datawatch",
             "secret_ref": "${secret:lifecycle-hook-prod}"}
  }' \
  https://localhost:8443/api/autonomous/subscriptions
```

**Comm channel:**

```text
autonomous subscribe task.completed,task.failed,prd.blocked sink=webhook target=https://hooks.example.net/datawatch secret_ref=${secret:lifecycle-hook-prod}
```

**PWA:** open an Automaton, choose the **Subscriptions** tab, then
**Add subscription**. Choose the event kinds, set the scope to all Automata
if the rule should cover every one, choose **Webhook** as the sink, enter the
**Webhook URL** and the **Secret reference** (`${secret:lifecycle-hook-prod}`),
and save. The form does not show the secret's value.

Creation is refused, with a message that names the cause, when:

- webhook delivery is disabled (`webhook delivery is disabled; set autonomous.notify.webhook.enabled to true …`);
- the URL is not `https` (plain `http` is accepted only for an allow-listed host);
- the URL contains a user name or password, or has no host;
- the host is not on the allow-list;
- no secret reference is given, or the value is a literal rather than a `${secret:name}` reference.

Test a rule before you rely on it:

```sh
datawatch autonomous subscription-test <subscription-id>
```

The test sends one signed event labelled `[test]` through the same path a real
event takes. It does not use up a one-shot rule.

## What the receiver gets

Every delivery is one `POST` to the URL with these headers:

| Header | Value |
|---|---|
| `Content-Type` | `application/json` |
| `User-Agent` | `datawatch-lifecycle/1` |
| `X-Datawatch-Signature` | `t=<unix seconds>,v1=<lowercase hex>` |
| `X-Datawatch-Delivery` | The delivery id: 32 hex characters. The same value on every retry of one delivery. |
| `X-Datawatch-Event` | The event kind, such as `task.completed` |

The body is a JSON envelope. The `event` object is the same lifecycle event the
other sinks receive:

```json
{
  "version": 1,
  "delivery_id": "0f3c9a6e1b7d4c2a8e5f9b0d6a1c3e7f",
  "sent_at": "2026-10-25T12:30:00Z",
  "host": "build-01",
  "subscription_id": "<subscription-id>",
  "event": {
    "id": "<event-id>",
    "at": "2026-10-25T12:29:58Z",
    "kind": "task.completed",
    "level": "task",
    "prd_id": "<automaton-id>",
    "task_id": "<task-id>",
    "old_status": "running",
    "new_status": "completed",
    "host": "build-01"
  }
}
```

The envelope fields are:

- `version` — the envelope version. It is `1` today; a new version adds fields
  and keeps the existing ones.
- `delivery_id` — the same value as `X-Datawatch-Delivery`.
- `sent_at` — when the first attempt was made. It is the same on every retry.
- `host` — the name of the daemon that sent the event.
- `subscription_id` — the subscription that matched.
- `event` — the lifecycle event itself.
- `truncated` — present and `true` only when the event's `decision` and
  `verdict` were removed to keep the body under the size cap (64 KiB).
- `forward` — present only on an event that arrived through peer forwarding
  (see [Peer forwarding](#peer-forwarding)).

**Signature.** The signed string is the timestamp, a full stop, and the raw
request body. The value is `hex(HMAC-SHA256(secret, "<t>" + "." + body))`.
Each attempt is signed with its own timestamp; the body does not change
between attempts.

**Verify it on the receiver.** Check the signature over the raw bytes you
received, before any JSON parsing. Then check the timestamp and de-duplicate
on the delivery id. This Python snippet is a complete check:

```python
import hashlib
import hmac
import time

TOLERANCE_SECONDS = 300  # autonomous.notify.webhook.signature_tolerance_seconds


def verify(secret: bytes, signature_header: str, raw_body: bytes) -> bool:
    fields = dict(part.split("=", 1) for part in signature_header.split(","))
    ts = fields["t"]
    if abs(time.time() - int(ts)) > TOLERANCE_SECONDS:
        return False  # too old or too far in the future: a replay, or clock skew
    expected = hmac.new(secret, ts.encode() + b"." + raw_body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, fields.get("v1", ""))
```

Pass the shared secret as bytes (UTF-8 encoded), and pass the body exactly as
it was received, not re-serialised JSON. Then:

1. Store the `X-Datawatch-Delivery` value and skip any delivery you have
   already handled. A retry after a lost response arrives a second time.
2. Answer with any `2xx` status once the event is stored. Answer quickly; a
   slow receiver is cut off after the attempt timeout and retried.

## Retries, failures and audit records

| Setting | Value | Notes |
|---|---|---|
| Attempts | 5 in total | `autonomous.notify.webhook.max_attempts` |
| Delays between attempts | 1 s, 4 s, 16 s, 64 s | Each delay has ±20 % random jitter. `backoff_seconds` and `jitter_percent` change them. |
| Time limit per attempt | 10 s | Connect, request and response together |
| Retried | Network errors, timeouts, `408`, `429`, any `5xx` | A `429` `Retry-After` is honoured when it is longer than the next delay, up to 120 s |
| Not retried | Any other `4xx`, any `3xx`, a payload over the cap, a refused host, an unresolvable secret | These fail at once |
| Payload cap | 64 KiB | An over-size event first loses its `decision` and `verdict`. If it is still too large, it is dropped without being sent. |

Redirects are never followed. A `3xx` is a permanent failure, so point the
URL at the final address.

A subscription's own retries run on that subscription's delivery worker. A
slow or failing receiver delays only its own subscription.

**When a delivery is dropped:**

- The subscription is kept. The next matching event tries again.
- A one-shot subscription is kept too. It is removed only after a successful
  delivery.
- One **alert** is raised, titled *Lifecycle webhook delivery failed*, at the
  error level. It appears in the alert list described in
  [Alerts and notifications](alerts-and-notifications.md).

**Audit records.** The audit log is `<data_dir>/audit.log`, one JSON object per
line. When the CEF mirror is enabled (`audit.cef_mirror_enabled`), the same
records are written to `audit.log.cef`, for a SIEM. The action names are:

| Record | JSON-lines action | CEF name | When |
|---|---|---|---|
| Each attempt | `autonomous_lifecycle_webhook_delivery` | `AutonomousLifecycleWebhookDelivery` | After every attempt, and for a refused or skipped delivery. `outcome` is `delivered`, `retry`, `failed`, `refused` or `skipped`. |
| Dropped delivery | `autonomous_lifecycle_webhook_failed` | `AutonomousLifecycleWebhookFailed` | Once, after the attempt records, when a delivery is dropped |

The fields are `subscription_id`, `delivery_id`, `attempt`, `max_attempts`,
`host` (host and port only), `status_code`, `outcome`, `error` and
`duration_ms`. The signing secret, the signature, the body, any user name in
the URL and the URL's query string are never recorded. A subscription that is
created, changed, tested or deleted is also audit-logged.

## SSRF rules and the allow-listed private host

The daemon will not make a request to an address it should not reach. These
rules run when the subscription is created, on every attempt, and again at
the moment the connection is made, so a name that changes its address later
is still caught.

- **Scheme.** `https` only. Plain `http` is accepted only for a host that is on
  the allow-list.
- **Address classes refused by default:** loopback (`127.0.0.0/8`, `::1`),
  link-local (`169.254.0.0/16`, `fe80::/10`), unspecified, multicast, private
  ranges (RFC 1918 `10/8`, `172.16/12`, `192.168/16`), IPv6 unique-local
  (`fc00::/7`) and Tailscale CGNAT (`100.64.0.0/10`).
- **Allow-listed private host.** The address classes above are allowed only
  when the host is on the allow-list, written as a name or as a literal IP. A
  receiver on your LAN is reachable this way:

  ```sh
  datawatch config set autonomous.notify.webhook.allow_hosts receiver.lan:8443
  ```

  Allow-listing a name trusts that name's DNS. Do not allow-list a name that
  you do not control.
- **Cloud metadata addresses stay refused.** `169.254.169.254` and
  `fd00:ec2::254` are refused even when their host is on the allow-list. This
  is the proposed behaviour, pending the operator's confirmation.
- **Obfuscated IP spellings are refused.** Decimal, octal, hex and short forms
  such as `2130706433`, `0177.0.0.1`, `0x7f.1` and `::ffff:127.0.0.1` are not
  accepted. Write the address in dotted-quad form, or use a name.
- **Proxies are ignored.** The request does not use the proxy environment
  variables.
- **Response size is capped.** The daemon reads only the first part of the
  receiver's response, which is used for diagnostics only.

## Peer forwarding

Peer forwarding sends a matching event to another datawatch daemon. You
register that daemon as a federation peer; the subscription names the peer,
and no address or token is stored in the subscription.

**Set up the two daemons:**

1. **On the sending daemon**, register the receiving daemon as a peer. Use the
   token that the receiving daemon issued for this daemon, and grant the
   `autonomous-operator` group (or a capability set that includes
   `autonomous:write`):

   ```sh
   datawatch federation peer add --name hub-east \
     --url https://hub-east.example.net:8443 \
     --token "<token issued to this daemon>" \
     --capabilities autonomous-operator
   ```

2. **On both daemons**, turn on forwarding (`autonomous.notify.forwarding.enabled`
   set to `true`). The sending daemon needs it to create a peer subscription.
   The receiving daemon needs it to accept forwarded events.
3. **On the receiving daemon**, register the sending daemon as a peer with the
   same token and the `autonomous-operator` group. Its token must be the one the
   sending daemon uses, or every forward is refused with `401`.

**Subscribe with the peer sink.** The target is the registered peer name. Letters,
digits, `.`, `_` and `-` are allowed, up to 64 characters. A peer subscription
carries no secret.

```sh
datawatch autonomous subscribe --on task.failed --on prd.blocked \
  --sink peer --target hub-east
```

```text
autonomous subscribe task.failed,prd.blocked sink=peer target=hub-east
```

In the PWA, choose **Federation peer** as the sink and pick the peer from the
list. Creation is refused, and the refusal names the failed condition, when the
peer is not registered, is not enabled, is not a federation peer, or does not
grant `autonomous:write`. The same checks run again on every delivery.

**What the sender sends.** The sender posts the same envelope as the webhook
sink to `<peer base URL>/api/autonomous/events/forward`. The request carries:

- `Authorization: Bearer <token>` for the peer;
- the same `X-Datawatch-Signature`, `X-Datawatch-Delivery` and `X-Datawatch-Event`
  headers, with the peer's token as the signing key;
- an `X-Datawatch-Hop-Chain` header that names the origin and the hops taken.

The envelope has a `forward` block: `origin_host` (the daemon the event started
on), `hops` (how many daemons have handled it, counting the sender) and
`visited` (the host names it has passed through).

**What the receiving daemon sees.** A forwarded event that is accepted is
answered with `202` and then:

- re-emitted to its own subscriptions. Local subscriptions of every sink type
  (comm, session, topic, alert, `mcp_queue` and webhook) see it like any other
  event;
- shown in human-readable sinks with the words `(forwarded from <origin host>)`;
- carried in `mcp_queue` events and in webhook envelopes with its `forward`
  block, so the origin is visible to your code;
- never written to the receiving daemon's Automaton state, and never used to
  drive an Automaton. It is a notification only.

**Loop prevention.** A forwarded event does not go out again.

- A daemon never forwards an event that it received as forwarded. The peer sink
  refuses it.
- A receiving daemon answers `409` and does not accept the event when:
  - its own name is the origin host;
  - its own name is already in the `visited` list;
  - the hop count has reached the cap (`autonomous.notify.forwarding.max_hops`,
    default `3`).
- A `409` is never retried by the sender. Every refusal is audited.

**When the peer is offline.** The sender retries with the same schedule as the
webhook sink (five attempts, 1 s, 4 s, 16 s and 64 s, with jitter). If the
peer is still unreachable after the last attempt, the event is dropped for that
peer, one alert is raised (*Lifecycle peer forward failed*), and the attempts
are audited. There is no outbox: forwarding is best-effort. The sending
daemon's own Automaton state is still the record, and the peer can read it
through the API once it is back. A daemon restart forgets any retry that is in
progress.

**Audit records for forwarding.** The sending side writes
`autonomous_lifecycle_forward_delivery` (CEF name
`AutonomousLifecycleForwardDelivery`) for each attempt and the final outcome.
The receiving side writes `autonomous_lifecycle_forward_receive` (CEF name
`AutonomousLifecycleForwardReceive`) for every forwarded request it answers,
accepted or refused. Neither record contains the bearer token, the signature or
the body.

## Troubleshooting

| Symptom | Likely cause | What to do |
|---|---|---|
| Creation refused: `host … is not on the webhook allow-list` | The receiving host is missing from the allow-list, or the entry has a scheme, path or port that does not match the URL | Add the exact host, or `host:port`, to `autonomous.notify.webhook.allow_hosts` on any surface. Check the URL's port. |
| Creation refused: `the webhook host allow-list is empty` | Nothing has been allow-listed yet | Add the receiving host. An empty list means no host can receive. |
| Creation refused: `webhook delivery is disabled` | The master switch is off | Set `autonomous.notify.webhook.enabled` to `true`. |
| Subscription exists but nothing arrives | The switch is off, or the host was removed from the allow-list after the subscription was created. Both are checked on every event. | Turn the switch on, or add the host back. A `refused` record in the audit log names the host. The subscription needs no change. |
| Alert and audit: `the signing secret could not be resolved` | The vault entry is missing, the name is misspelled, or the vault is locked | Run `datawatch secrets list`, check the name in `${secret:name}`, and set the entry again. |
| Creation refused: `a literal value is refused` | A secret value was typed in place of a reference | Store the value in the vault and reference it as `${secret:name}`. |
| Receiver rejects the signature | A different secret from the vault, a body that was parsed and re-serialised before checking, or clocks more than 300 s apart | Use the same secret value, check the raw body, and check the time on both hosts (NTP). |
| Receiver gets the same event twice | A retry after a lost response | Skip a delivery id you have already handled. |
| Audit shows `refused` with `refusing to connect to a private …` or `is a loopback` | The host resolves to a blocked address class | Use a public name, or allow-list the private host explicitly. Metadata addresses are never allowed. |
| Audit shows `host … did not resolve` | The name does not resolve from the daemon | Check DNS from the daemon's host. |
| Peer subscription refused: `is not a registered, enabled federation peer` | The peer is missing or disabled | Check `datawatch federation peer list` and enable the peer. |
| Peer subscription refused: `does not grant the autonomous:write capability` | The peer's capabilities do not include `autonomous:write` | Set the peer's capabilities to `autonomous-operator`. |
| Peer answers `401` | Missing or wrong bearer token, or a signature made with a different token | Make the token the same on both daemons. Re-issue it if it has been lost. |
| Peer answers `403` | The sending daemon's token is not granted `autonomous:write` on the receiver | Grant the `autonomous-operator` group to that peer on the receiving daemon. |
| Peer answers `409` | A loop rule: the origin is the receiver, the receiver is already in `visited`, or the hop cap was reached | Look for a loop of forwarding peers and remove one link. Raise `max_hops` only if the topology needs it. |
| Peer answers `400` | The envelope is malformed, or it is over the size cap | Check that both daemons use the same `max_body_bytes`. |
| Peer answers `503` | Forwarding is off on the receiver, or its lifecycle engine is not wired. The sender treats `503` as temporary and retries. | Turn forwarding on at the receiver. A permanent `503` ends in a dropped delivery and an alert. |
| Alert: `payload … over the … byte cap` | The event is still too large after its `decision` and `verdict` are removed | Raise `autonomous.notify.webhook.max_body_bytes` (on both daemons when forwarding). |
| Peer offline, events missing on the peer | Forwarding has no outbox. A dropped event is not replayed. | Read the Automaton state through the API. Watch the forward alert and audit to see which events were dropped. |

## See also

- [Automata lifecycle subscriptions](automaton-lifecycle-subscriptions.md) — the filter, the sinks and the in-app delivery paths
- [Flow: lifecycle event subscriptions](../flow/lifecycle-subscription-flow.md) — the pipeline, with the webhook and peer paths
- [Secrets manager](secrets-manager.md) — storing and rotating the signing secret
- [Federated observer](federated-observer.md) — registering and granting federation peers
- [Alerts and notifications](alerts-and-notifications.md) — where dropped-delivery alerts appear
- [Configuration reference](../config-reference.yaml) — every `autonomous.notify.webhook.*` and `autonomous.notify.forwarding.*` key
- [Lifecycle webhook and forwarding proposal](../plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md) — the design decisions that are pending confirmation
