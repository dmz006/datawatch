# Covert / Low-Profile Communication Channels — Research Notes

This document evaluates alternative communication channels for datawatch that
operate under constrained network environments (firewalls, outbound-only, no
open ports) or where a low observable footprint is desirable.

> **Status**: research / planning only. None of these are implemented.
> See `BACKLOG.md` for prioritisation.

---

## Motivation

datawatch's existing backends (Signal, Telegram, Slack, webhooks, web UI) all
require either inbound TCP (webhooks, web UI) or outbound HTTPS to third-party
services. In environments where:

- All inbound TCP is blocked by firewall
- Third-party messaging services are unavailable or undesirable
- Traffic must blend with existing infrastructure

…alternative channels may be needed.

---

## DNS Tunneling

**Concept:** Encode commands and responses in DNS queries/responses to a
controlled domain. The datawatch server runs an authoritative DNS resolver for
a subdomain (e.g. `ctl.example.com`); the client sends commands as DNS TXT or
A-record queries; the server responds via DNS answers.

**Advantages:**
- DNS is rarely blocked outbound (UDP 53 / TCP 53)
- Blends with legitimate DNS traffic
- No inbound ports required on the server
- Works from heavily firewalled environments

**Implementation sketch:**
- Backend type: `dns`
- Server role: authoritative NS for a delegated subdomain via `miekg/dns`
- Client role: standard `net.Resolver` queries with TXT encoding
- Encoding: base64url payload fragmented across labels; reassembly on the server
- Auth: HMAC-SHA256 of payload + shared secret in a nonce label
- Throughput: low (~100 bytes/query), suitable for short commands only; not
  suitable for streaming session output

**Limitations:**
- High latency (DNS TTL, caching, resolver hops)
- Low bandwidth — not suitable for tailing session output
- Requires control of a delegated DNS zone
- DNSSEC signing adds complexity but improves integrity guarantees

**References:**
- [iodine](https://github.com/yarrick/iodine) — DNS tunnel reference
- [dns2tcp](https://github.com/alex-sector/dns2tcp)
- RFC 4034 — DNSSEC Resource Records

---

## ICMP Tunneling

**Concept:** Encode payloads in ICMP Echo Request/Reply packets.

**Advantages:** ICMP is permitted through many firewalls; no TCP state needed.

**Limitations:** Requires raw socket (root/CAP_NET_RAW); many cloud providers
and corporate networks drop ICMP or rate-limit it; implementation is complex.
Not recommended for production use.

---

## NTP-based Side Channel

**Concept:** Embed data in NTP timestamp fields or extension fields.

**Limitations:** Extremely low bandwidth; NTP traffic monitoring is increasingly
common; non-standard extension use triggers anomaly detection. Not practical.

---

## HTTPS to Inconspicuous Endpoints

**Concept:** Use legitimate-looking HTTPS POSTs to a controlled server endpoint
that mimics a CDN or analytics service.

**Advantages:**
- HTTPS port 443 is almost universally permitted outbound
- TLS encrypts payload content
- No special infrastructure beyond a VPS with a TLS cert

**Limitations:** Not meaningfully different from the existing webhook backend
except for the endpoint's appearance. This is essentially the current webhook
backend.

---

## Steganographic Channels

**Concept:** Embed commands in cover traffic — e.g. image EXIF data, HTTP
headers, DNS PTR records.

**Limitations:** Fragile, high engineering cost, low reliability, not
appropriate for an operations tool.

---

## Recommended Approach

For the backlog DNS channel, the minimum viable design is:

1. `internal/messaging/backends/dns/` — new backend implementing
   `messaging.Backend`
2. Server mode: uses `miekg/dns` to serve authoritative responses for a
   configured subdomain
3. Client mode (CLI `--server <name>` with `type: dns`): encodes commands as
   TXT queries, polls for responses
4. Config block:

```yaml
dns_channel:
  enabled: false
  mode: server                  # server | client
  domain: ctl.example.com       # delegated subdomain
  listen: ":53"                 # server only
  upstream: 8.8.8.8:53          # client: resolver to use
  secret: ""                    # HMAC shared secret
```

5. Commands are short enough (< 200 bytes) to fit in DNS; session output
   tailing is not supported over this channel

---

## See Also

- `BACKLOG.md` — prioritisation of DNS channel implementation
- `docs/messaging-backends.md` — existing backend implementations
- `internal/messaging/backend.go` — Backend interface to implement
