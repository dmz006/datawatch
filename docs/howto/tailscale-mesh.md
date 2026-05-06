# How-to: Tailscale Mesh — private overlay network for agents

Spawn container workers in a Kubernetes cluster and have them join a
private Tailscale (or Headscale) mesh automatically. Datawatch + the
agent fleet end up on the same private tailnet; no public exposure
required, no per-cluster VPN to set up.

## What it is

A Tailscale sidecar (Headscale-compatible) injected into agent worker
pods at spawn time. The pod's main process can reach the datawatch
host (and any other peer in the tailnet) on private 100.x.y.z IPs.
Operator sees the agent in `datawatch tailscale nodes`; agent sees
the host as another node in the mesh.

**Headscale vs commercial Tailscale.** Headscale is the recommended
self-hosted control plane (free, you own the keys); commercial
Tailscale is the hosted alternative (free tier generous; managed for
you). Both speak the same protocol.

## Base requirements

- `datawatch start` — daemon up.
- A Kubernetes cluster reachable from the daemon (a Cluster Profile in
  `~/.datawatch/profiles/clusters/` — see `profiles.md`).
- Headscale running OR a commercial Tailscale account.
- A pre-auth key from your control plane (Headscale: `headscale
  preauthkeys create --reusable --expiration 0`; Tailscale admin
  console → **Keys**).
- Outbound UDP 41641 from agent pods (Tailscale's preferred direct
  port; falls back to DERP relay if blocked).

## Setup

```sh
# 1. Run Headscale (skip if using commercial Tailscale).
docker run -d --name headscale \
  -v $HOME/.headscale:/etc/headscale -p 8080:8080 \
  --restart=unless-stopped \
  headscale/headscale:latest headscale serve

# 2. Create user + reusable, non-expiring pre-auth key.
docker exec -it headscale headscale users create datawatch
docker exec -it headscale headscale preauthkeys create \
  --user datawatch --reusable --expiration 0
#  → tskey-...

# 3. Store the key in the secrets manager.
datawatch secrets set TS_PREAUTH_KEY "tskey-..."
```

Configure datawatch:

```yaml
# ~/.datawatch/datawatch.yaml
agents:
  tailscale:
    enabled: true
    control_plane_url: http://headscale.your.host:8080
    preauth_key: ${secret:TS_PREAUTH_KEY}
    hostname_prefix: dw-agent
    tags:
      - tag:datawatch-agent
      - tag:env:lab
```

Restart: `datawatch restart`.

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Confirm Tailscale is configured + the daemon node is healthy.
datawatch tailscale status
#  → node: dw-host-kona  ip: 100.64.0.1  status: connected
#    control plane: http://headscale.your.host:8080  reachable: yes

# 2. Spawn an agent worker — it auto-joins.
datawatch sessions start \
  --backend claude-code \
  --agent k8s \
  --cluster lab-east \
  --task "Run integration tests"
#  → spawned session ralfthewise-abcd; agent dw-agent-abcd joining mesh ...

# 3. List peers.
sleep 10
datawatch tailscale nodes
#  → dw-host-kona       100.64.0.1   self
#    dw-agent-abcd      100.64.0.42  online (1m ago)

# 4. Generate + push an ACL that locks down the fleet.
datawatch tailscale acl-generate > /tmp/acl-preview.json
datawatch tailscale acl-push --dry-run     # show diff vs current
datawatch tailscale acl-push               # actually push

# 5. Prune stale nodes (after sessions finish + pods terminate).
datawatch tailscale prune --older-than 24h
```

### 4b. Happy path — PWA

1. PWA → Settings → Agents → **Tailscale Configuration** card. Confirm
   all fields populated:
   - Control plane URL, pre-auth key reference, hostname prefix, tags.
   - **Validate** button does a health check against the control plane.
2. Scroll to **Mesh Status** card below. Shows this host's tailnet
   identity + peer list.
3. Spawn an agent: PWA → Sessions → **+** FAB → choose `agent: k8s`,
   pick the cluster from the Cluster Profile dropdown, set the task,
   click **Start**.
4. Watch the **Mesh Status** card refresh — the new agent appears in
   the peer list within ~10 s with a green online dot.
5. To push an ACL, scroll to the **ACL** subsection of Tailscale
   Configuration → **Generate** previews the JSON in an inline editor;
   **Push** sends it to the control plane.
6. To remove a stale agent node from the control plane, click × on
   the peer row in Mesh Status.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Settings → Agents → Tailscale Configuration + Mesh Status cards.
Read-mostly recommended — ACL push is best from CLI / PWA where you
can preview the JSON cleanly.

### 5b. REST

```sh
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/tailscale/status
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/tailscale/nodes
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -d '{"dry_run":false}' $BASE/api/tailscale/acl-push
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -d '{"older_than":"24h"}' $BASE/api/tailscale/prune
```

### 5c. MCP

Tools: `tailscale_status`, `tailscale_nodes`, `tailscale_acl_push`.

Useful in autonomous flows where the LLM operator wants to verify
mesh health before kicking off a multi-agent workflow:

> *Operator AI:* (`tailscale_status` → confirms control plane reachable)
> (`tailscale_nodes` → confirms the cluster's agent set is online)
> (then proceeds to spawn the workflow)

### 5d. Comm channel

| Verb | Example |
|---|---|
| `tailscale status` | One-line health for chat. |
| `tailscale nodes` | Peer list as a chat block. |
| `tailscale prune older=24h` | Cleanup. |
| `tailscale acl-push dry=true` | Preview-only diff. |

ACL push without `dry=true` is an operator-typed-in-CLI operation only —
not exposed via chat for safety.

### 5e. YAML

`agents.tailscale.*` block in `~/.datawatch/datawatch.yaml`:

```yaml
agents:
  tailscale:
    enabled: true
    control_plane_url: ...
    preauth_key: ${secret:TS_PREAUTH_KEY}
    hostname_prefix: dw-agent
    tags: [tag:datawatch-agent, tag:env:prod]
    acl_oauth_client: ${secret:TS_OAUTH_CLIENT}    # commercial Tailscale only
```

Operator-added ACL rules are preserved across `acl-push` if wrapped in
markers:

```jsonc
"acls": [
  // #operator-rules-begin
  { "action": "accept", "src": ["operator@example.com"], "dst": ["tag:datawatch-agent:22"] },
  // #operator-rules-end
  // (datawatch auto-generated rules below)
]
```

Restart needed after editing config: `datawatch restart`.

## Diagram

```
  ┌─────────────────┐
  │ Headscale or    │ ← control plane (key issuer + ACL)
  │ Tailscale cloud │
  └────┬────────────┘
       │ pre-auth key
       │
  ┌────▼───────────┐                ┌────────────────┐
  │ datawatch host │ ◄────tailnet──►│ agent pod (TS  │
  │  (this node)   │  100.x.y.z     │  sidecar) × N   │
  └────────────────┘                └────────────────┘
                          │
                          │ private; no public ports
                          ▼
                ┌────────────────┐
                │ Kubernetes     │
                │ cluster        │
                └────────────────┘
```

## Common pitfalls

- **Pre-auth key expired.** Headscale + commercial Tailscale both let
  you create non-expiring keys (`--expiration 0` for Headscale; admin
  console toggle for commercial). Use those for agent fleets.
- **UDP 41641 blocked.** Tailscale prefers direct UDP; falls back to
  DERP relay if blocked. Works but slower. Open UDP 41641 outbound on
  the agent cluster.
- **Operator-added ACL rules wiped by `acl-push`.** Wrap in the
  `#operator-rules` markers; the generator preserves anything between.
- **Two control planes on one host.** Can't run Headscale + point at
  commercial Tailscale at the same time. Pick one.
- **Node IP visible in logs.** The 100.x.y.z address is fine to log;
  it's only routable inside the tailnet. Don't paste publicly.

## Linked references

- See also: `container-workers.md` for the agent worker walkthrough
- See also: `secrets-manager.md` for storing the pre-auth key
- See also: `profiles.md` for Cluster Profiles
- Architecture: `../architecture-overview.md` § Container workers + mesh

## Screenshots needed (operator weekend pass)

- [ ] PWA Tailscale Configuration card with all fields
- [ ] Mesh Status card showing this host + agent peers
- [ ] CLI `datawatch tailscale status` output
- [ ] Headscale admin showing the auto-joined agent node
- [ ] ACL preview output (dry-run JSON)
- [ ] Architecture diagram of the mesh sidecar (mermaid)
