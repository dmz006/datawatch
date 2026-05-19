---
docs:
  index: true
  topics: [channel-routing, federation, comms, bl331, channel-identity, owner-peer]
exec_params:
  - name: peer_name
    description: "Federation peer name to associate with a channel identity"
    required: false
exec_steps:
  - tool: get_config
    description: Check current channel routing rules
    args: {}
    read_only: true
---
# How-to: Channel-Address Federation via Comms (BL331)

Channel-address federation lets you route inbound messages from a known
channel identity — a Telegram group, a Signal number, a Matrix room — to
a specific federation peer, with an optional automata type and default
project directory. Sessions and PRDs created through this routing carry an
`owner_peer` field that tracks which federated peer originated the request.

## Overview

```
Inbound message (Telegram group -1001234567890)
         │
         ▼
  channel_routing.json  ← PUT /api/channel/routing
         │  matches channel_pattern
         ▼
  federation peer "nas-peer"  ← has channel_identity set
         │
         ▼
  Session / PRD  ← owner_peer = "nas-peer"
```

When a message arrives on a comm channel, datawatch checks the routing
rules in order. The first rule whose `channel_pattern` matches the
inbound channel address wins. The daemon proxies or forwards the request
to the named federation peer, and any session or PRD created as a result
has `owner_peer` set to that peer's name.

This is useful when you want different Telegram groups (or Signal numbers)
to dispatch to different servers — a home server for personal projects,
a NAS peer for long-running builds, a cloud peer for GPU tasks.

## Prerequisites

- A running datawatch daemon (see [`setup-and-install.md`](setup-and-install.md)).
- At least one federation peer registered (see [`federated-observer.md`](federated-observer.md)).
- Bearer token with `comm:write` to configure routing rules.

---

## 1 — Add a federation peer with channel_identity

The `channel_identity` field on a federation peer is a list of channel
addresses that should be treated as "belonging to" that peer. This is
informational metadata used by the routing engine.

### REST

```sh
# Register a new peer with channel_identity.
curl -sk -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "nas-peer",
    "url": "https://nas.local:8443",
    "token": "nas-peer-secret",
    "capabilities": "comms-channel-agent",
    "channel_identity": [
      "telegram:group:-1001234567890",
      "signal:+15551234567"
    ]
  }' \
  https://datawatch.local:8443/api/federation/peers
# {"id":"peer-...","name":"nas-peer","ok":true}
```

To update an existing peer's channel_identity:

```sh
PEER_ID=peer-<id>
curl -sk -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"channel_identity":["telegram:group:-1001234567890"]}' \
  https://datawatch.local:8443/api/federation/peers/$PEER_ID
```

### CLI

```sh
# Add peer with channel identity.
datawatch federation peer add \
  --name "nas-peer" \
  --url "https://nas.local:8443" \
  --token "nas-peer-secret" \
  --capabilities "comms-channel-agent" \
  --channel-identity "telegram:group:-1001234567890"

# Update existing peer.
datawatch federation peer update nas-peer \
  --channel-identity "telegram:group:-1001234567890,signal:+15551234567"
```

### PWA

1. Open **Settings → Comms → Remote Servers**.
2. Click **Add** (or the edit icon on an existing peer).
3. Enable the **Federated peer** toggle.
4. In the **Channel Identity** text input, enter the channel addresses
   separated by commas (e.g. `telegram:group:-1001234567890`).
5. Click **Save**.

### MCP

```
federation_peer_add(
  name="nas-peer",
  url="https://nas.local:8443",
  token="nas-peer-secret",
  capabilities="comms-channel-agent",
  channel_identity=["telegram:group:-1001234567890"]
)
```

---

## 2 — Set up channel routing rules

Routing rules live in `~/.datawatch/channel_routing.json` and are managed
via `GET /api/channel/routing` and `PUT /api/channel/routing`.

### REST

```sh
# View current rules (empty on fresh install).
curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  https://datawatch.local:8443/api/channel/routing | jq .
# {"rules":[]}

# Set a routing rule: Telegram group → nas-peer, automata_type=feature.
curl -sk -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "rules": [
      {
        "channel_pattern": "telegram:group:-1001234567890",
        "peer_name": "nas-peer",
        "automata_type": "feature",
        "default_project_dir": "/workspace/myapp"
      }
    ]
  }' \
  https://datawatch.local:8443/api/channel/routing
# {"ok":true}

# Confirm.
curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  https://datawatch.local:8443/api/channel/routing | jq .rules
```

### CLI

```sh
# List current rules.
datawatch channel routing list

# Add a rule.
datawatch channel routing add \
  --pattern "telegram:group:-1001234567890" \
  --peer "nas-peer" \
  --automata-type "feature" \
  --project-dir "/workspace/myapp"
```

### PWA

1. Open **Settings → Comms → Channel Routing**.
2. The card lists all current rules; each shows pattern, peer, automata type.
3. Click **Add Rule** to open the form.
4. Fill in **Channel Pattern**, **Peer Name**, optionally **Automata Type**
   and **Default Project Dir**.
5. Click **Save**.

### MCP

```
# There is no dedicated routing MCP tool; use the REST surface via ask or assist.
ask("show me the current channel routing rules")
# or
assist("add a routing rule for telegram group -1001234567890 to nas-peer")
```

---

## 3 — The comms-channel-agent capability group

BL331 adds a 14th builtin federation capability group: `comms-channel-agent`.
It grants `comm:read + comm:write` — the same surface as `comm-bridge` from
v8.2.0, but semantically distinct: use `comms-channel-agent` for peers that
act as channel-routing agents, and `comm-bridge` for generic push/notification
bridge peers.

```sh
# Verify the group is present (should be 14 entries).
curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  https://datawatch.local:8443/api/federation/groups/builtins | jq 'length'
# 14

# Inspect what comms-channel-agent grants.
curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  https://datawatch.local:8443/api/federation/groups/builtins \
  | jq '.[] | select(.name=="comms-channel-agent")'
```

---

## 4 — Example: Telegram group → federation peer routing

This end-to-end example routes messages from a Telegram group to a secondary
NAS peer that handles long-running builds.

```sh
# Step 1: Register the NAS peer.
curl -sk -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "nas-builds",
    "url": "https://192.168.1.50:8443",
    "token": "nas-secret-token",
    "capabilities": "comms-channel-agent",
    "channel_identity": ["telegram:group:-1009876543210"]
  }' \
  https://datawatch.local:8443/api/federation/peers

# Step 2: Set the routing rule.
curl -sk -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "rules": [{
      "channel_pattern": "telegram:group:-1009876543210",
      "peer_name": "nas-builds",
      "automata_type": "feature",
      "default_project_dir": "/mnt/data/workspace/myapp"
    }]
  }' \
  https://datawatch.local:8443/api/channel/routing

# Step 3: Send a message to the Telegram group.
# Telegram → datawatch (via Telegram bot) → routing match → forwarded to nas-builds

# Step 4: Check sessions on the NAS peer — owner_peer will be set.
curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  https://datawatch.local:8443/api/sessions \
  | jq '.[] | select(.owner_peer != null) | {id, owner_peer}'
```

---

## 5 — owner_peer on sessions and PRDs

When a session or PRD is created via a channel routing match, the `owner_peer`
field records which federation peer originated the request. You can filter
sessions and PRDs by owner.

```sh
# List sessions, show owner_peer field.
curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  https://datawatch.local:8443/api/sessions | jq '.[] | {id, name, owner_peer}'

# List PRDs, show owner_peer field.
curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  https://datawatch.local:8443/api/autonomous/prds | jq '.[] | {id, title, owner_peer}'
```

The `owner_peer` field is `null` (or absent) for sessions created locally. It
is set to the `peer_name` from the matching routing rule when the session was
created via a routed channel message.

---

## Federation access control

| Endpoint | Required capability |
|---|---|
| `GET /api/channel/routing` | `comm:read` |
| `PUT /api/channel/routing` | `comm:write` |

The `comms-channel-agent` and `comm-bridge` builtin groups both satisfy
these requirements. The `read-only` group satisfies `comm:read` (GET only).
`federation-peer` (the default safe group) has no `comm:*` caps — a
vanilla peer cannot read or write routing rules unless its capabilities
are explicitly upgraded.

---

## Troubleshooting

**Rules not matching.** Verify the `channel_pattern` exactly matches the
inbound address format (`telegram:group:<id>`, `signal:<phone>`, etc.). Use
`datawatch channel routing list` to confirm the stored rules.

**403 on PUT.** Your token or peer token lacks `comm:write`. Upgrade the
peer's capabilities to include `comms-channel-agent` or `comm-bridge`, or
use a token that has `comm:write`.

**owner_peer is null even after routing.** The session was created locally
(not via a routing match) or the daemon did not find a matching rule for
the inbound channel address.

---

## See also

- [`howto/federated-observer.md`](federated-observer.md) — register federation peers
- [`howto/comm-channels.md`](comm-channels.md) — configure all 11 messaging backends
- [`howto/file-service.md`](file-service.md) — share files across federation peers
- [`docs/datawatch-definitions.md`](../datawatch-definitions.md) — capability group reference
