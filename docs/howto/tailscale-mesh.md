# How-to: Tailscale Mesh — private overlay network for agents

Spawn container workers in a Kubernetes cluster and have them join a
private Tailscale (or Headscale) mesh automatically. Datawatch + the
agent fleet end up on the same private tailnet; no public exposure
required, no per-cluster VPN to set up.

## What it is

A Tailscale sidecar (Headscale-compatible) injected into agent worker
pods at spawn time. The pod's main process can reach the datawatch
host (and any other peer in the tailnet) on private 100.x.y.z IPs.
Operator sees the agent as another node in `datawatch tailscale nodes`;
agent sees the host as another node.

## Why

Without Tailscale: agent pods need a public route back to datawatch
(NAT'd ingress, port-forward, or VPN per cluster). With Tailscale: the
mesh handles NAT traversal, the agent pod sees datawatch as
`100.64.x.y`, and you don't expose anything publicly.

## Headscale vs commercial Tailscale

- **Headscale** (recommended for self-hosted) — open-source control
  plane you run yourself. Free; you own the keys. Good when you don't
  want to depend on a third party.
- **Tailscale** (commercial) — hosted control plane. Free tier
  generous; managed for you. Good when you want zero ops on the
  control plane.

Both speak the same protocol. Datawatch supports either; switch via
the control-plane URL in config.

## 1. Set up Headscale (self-hosted path)

If you don't already have a control plane:

```sh
docker run -d --name headscale \
  -v $HOME/.headscale:/etc/headscale \
  -p 8080:8080 \
  --restart=unless-stopped \
  headscale/headscale:latest \
  headscale serve
```

Initial config (one-time):

```sh
docker exec -it headscale headscale users create datawatch
docker exec -it headscale headscale preauthkeys create \
  --user datawatch --reusable --expiration 0
# → tskey-...
```

Save that pre-auth key — you'll feed it to datawatch in step 3.

Skip this step if using commercial Tailscale; generate a pre-auth
key from the admin console → **Keys**.

## 2. Verify the agent base image carries `tailscale`

Default datawatch agent image (`datawatch/agent:<version>`) carries
`tailscale` + `tailscaled` binaries already. If you've forked the
image, ensure both are present.

## 3. Register the pre-auth key as a secret

```sh
datawatch secrets set TS_PREAUTH_KEY "tskey-..."
```

(See [`secrets-manager.md`](secrets-manager.md) for the full secrets
walkthrough.)

## 4. Configure Tailscale in the agent block

`~/.datawatch/datawatch.yaml`:

```yaml
agents:
  tailscale:
    enabled: true
    control_plane_url: http://headscale.your.host:8080  # or https://controlplane.tailscale.com
    preauth_key: ${secret:TS_PREAUTH_KEY}
    hostname_prefix: dw-agent                            # nodes appear as dw-agent-<short-id>
    tags:                                                # optional; for ACL
      - tag:datawatch-agent
      - tag:env:lab
```

`datawatch restart` to apply.

PWA: Settings → Agents → **Tailscale Configuration** card exposes the
same fields with inline validation.

## 5. Spawn a worker — it auto-joins

```sh
datawatch sessions start \
  --backend claude-code \
  --agent k8s \
  --cluster lab-east \
  --task "Run integration tests"
```

The agent pod boots, the sidecar registers with the control plane
using your pre-auth key, the pod gets a `100.x.y.z` address and
becomes reachable from datawatch.

Verify:

```sh
datawatch tailscale status               # this host's view of the tailnet
datawatch tailscale nodes                # all peers
```

You should see the agent listed.

## 6. ACL generation

By default Tailscale lets every node talk to every node. For an
agent fleet you usually want tighter rules: agents talk to datawatch
and to each other (for cross-host caller attribution), but nothing
else.

`datawatch tailscale acl-generate` reads current node tags + agent
fleet membership and prints a Tailscale ACL JSON to stdout. Push it:

```sh
datawatch tailscale acl-generate                   # preview
datawatch tailscale acl-push --dry-run             # show diff vs current
datawatch tailscale acl-push                       # actually push
```

Headscale: pushed via the control-plane API. Commercial Tailscale:
pushed via OAuth client (configure `agents.tailscale.acl_oauth_client`).

The generated ACL preserves operator-added rules in the
`#operator-rules` section if present; only the auto-generated section
gets replaced.

## 7. Per-node tags + ACL

Tags from `agents.tailscale.tags` apply to every spawned agent. Use
them in ACL rules:

```jsonc
{
  "tagOwners": {
    "tag:datawatch-agent": ["datawatch"]
  },
  "acls": [
    {
      "action": "accept",
      "src":    ["tag:datawatch-agent"],
      "dst":    ["datawatch:443"]
    },
    {
      "action": "accept",
      "src":    ["tag:datawatch-agent"],
      "dst":    ["tag:datawatch-agent:*"]
    }
  ]
}
```

Add `#operator-rules` markers to preserve manual entries across
`datawatch tailscale acl-push`:

```jsonc
"acls": [
  // #operator-rules-begin
  { "action": "accept", "src": ["operator@example.com"], "dst": ["tag:datawatch-agent:22"] },
  // #operator-rules-end

  // (datawatch auto-generated rules below)
]
```

## 8. Drain + remove a node

When an agent finishes its session the pod terminates and the
Tailscale node entry goes stale. Clean up:

```sh
datawatch tailscale prune --older-than 24h
```

removes stale nodes from the control plane.

## CLI reference

```sh
datawatch tailscale status                       # this host's tailnet view
datawatch tailscale nodes                        # all peers
datawatch tailscale acl-generate                 # build ACL → stdout
datawatch tailscale acl-push [--dry-run]         # push to control plane
datawatch tailscale prune [--older-than 24h]    # remove stale nodes
datawatch tailscale auth-key-rotate              # generate + register a new pre-auth key
```

## REST / MCP

- `GET /api/tailscale/status` → status
- `GET /api/tailscale/nodes` → peers
- `POST /api/tailscale/acl-push` → push generated ACL
- MCP: `tailscale_status`, `tailscale_nodes`, `tailscale_acl_push`.

## Common pitfalls

- **Pre-auth key expired.** Headscale + commercial Tailscale both let
  you create non-expiring keys (`--expiration 0` for Headscale; admin
  console toggle for commercial). Use those for agent fleets.
- **Agent IP visible in logs.** The 100.x.y.z address is fine to log;
  it's only routable inside the tailnet. Don't paste it on a public
  bug tracker, but no security risk.
- **Cluster firewall blocks UDP 41641.** Tailscale prefers direct
  UDP; with that blocked it falls back to a relay (DERP). Works but
  slower. Open UDP 41641 outbound on the agent cluster.
- **Operator-added ACL rules wiped by `acl-push`.** Wrap them in
  `#operator-rules-begin` / `#operator-rules-end` markers; the
  generator preserves anything between those.
- **Two control planes on one host.** You can't run Headscale and
  point at commercial Tailscale at the same time on one machine. Pick
  one.

## Linked references

- Plan: BL243 Tailscale Mesh arc
- See also: `container-workers.md` for the full agent worker walkthrough
- See also: `secrets-manager.md` for storing the pre-auth key
- Architecture: `architecture-overview.md` § Container workers + mesh

## All channels reference

| Channel | How |
|---|---|
| **PWA** | Settings → Agents → Tailscale Configuration card + Mesh Status card. |
| **Mobile** | Same surface (read-mostly; ACL push best from CLI). |
| **REST** | `GET /api/tailscale/{status,nodes}`, `POST /api/tailscale/{acl-push,prune,auth-key-rotate}`. |
| **MCP** | `tailscale_status`, `tailscale_nodes`, `tailscale_acl_push`. |
| **CLI** | `datawatch tailscale {status,nodes,acl-generate,acl-push,prune,auth-key-rotate}`. |
| **Comm** | `tailscale status`, `tailscale nodes` for read-only inspection. |
| **YAML** | `agents.tailscale` block in `~/.datawatch/datawatch.yaml`. |

## Screenshots needed (operator weekend pass)

- [ ] PWA Tailscale Configuration card with all fields
- [ ] Mesh Status card showing this host + agent peers
- [ ] CLI `datawatch tailscale status` output
- [ ] Headscale admin showing the auto-joined agent node
- [ ] ACL preview output
