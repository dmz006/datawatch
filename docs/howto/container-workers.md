# How-to: Container workers

Spawn workers as Docker containers (or Kubernetes Pods) that run a
session-equivalent LLM workload, join the Tailscale mesh, push
observer stats back to the host, and tear down when the session
finishes. Use for sandbox isolation, multi-cluster fan-out, or
running an agent on a different OS / architecture than the daemon
host.

## What it is

An "agent worker" is a container running:

- The datawatch agent binary (a smaller-surface companion to the
  full daemon).
- Tailscale sidecar (joins the mesh; gives the agent a private
  100.x.y.z address).
- One LLM backend (claude-code, opencode-acp, ollama-local, etc.).
- Per-pod auth via PQC bootstrap key + per-spawn token.

The host daemon spawns the container, waits for it to register, then
delegates session work to it. Output streams back over the mesh.

## Base requirements

- `datawatch start` — daemon up.
- For Docker workers: `docker` daemon reachable.
- For Kubernetes workers: a Cluster Profile pointing at a working
  kubeconfig (see [`profiles.md`](profiles.md)).
- Tailscale or Headscale control plane configured (see
  [`tailscale-mesh.md`](tailscale-mesh.md)) — agents auto-join.
- Agent base image (`datawatch/agent:<version>` from GHCR; or your
  own fork).

## Setup

```sh
# 1. Configure the agent block.
cat >> ~/.datawatch/datawatch.yaml <<'EOF'
agents:
  enabled: true
  default_driver: k8s              # or docker
  image: datawatch/agent:latest
  image_pull_policy: IfNotPresent
  pqc_bootstrap_key: ${secret:PQC_BOOTSTRAP_KEY}
  tailscale:
    enabled: true
    control_plane_url: http://headscale.your.host:8080
    preauth_key: ${secret:TS_PREAUTH_KEY}
EOF

# 2. Generate the PQC bootstrap key + store as secret.
datawatch agents pqc-keygen
#  → bootstrap key written to secrets manager as PQC_BOOTSTRAP_KEY

# 3. Reload.
datawatch reload
```

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Confirm agent subsystem health.
datawatch agents status
#  → driver: k8s; reachable: yes; ts_mesh: connected
#    pqc_bootstrap_key: present (rotated 2026-04-30)

# 2. Spawn a worker for a one-off task.
SID=$(datawatch sessions start \
  --backend claude-code \
  --agent k8s \
  --cluster lab-east \
  --profile datawatch-dev \
  --task "Run integration tests across the cluster" 2>&1 \
  | grep -oP 'session \K[a-z0-9-]+')

# 3. Wait for the agent to boot + join the mesh.
sleep 15
datawatch agents list
#  → dw-agent-7a3c   session=ralfthewise-7a3c   cluster=lab-east   ts_ip=100.64.0.42   status=running

# 4. Stream output back from the agent through the mesh.
datawatch sessions tail $SID -f

# 5. Get attestation chain (PQC bootstrap → spawn token → session).
datawatch agents attestation $SID
#  → root: PQC_BOOTSTRAP_KEY (rotated 2026-04-30)
#    leaf: spawn-token-7a3c (issued 2026-05-06T14:00:00Z)
#    chain: [verified]

# 6. Stop / clean up.
datawatch sessions kill $SID
# Pod terminates automatically when session ends; manual force-kill:
datawatch agents terminate dw-agent-7a3c
```

### 4b. Happy path — PWA

1. Settings → Agents → **Container Workers** card. Confirm fleet
   status: driver, reachable, mesh state.
2. To spawn an agent-backed session: Sessions → + FAB → wizard:
   - Backend: claude-code (or whichever)
   - **Agent**: dropdown — `local` (default) / `docker` / `k8s`
   - When agent is k8s: **Cluster** dropdown shows your Cluster
     Profiles
   - Profile (Project): pick a Project Profile
   - **Start**.
3. Detail view opens. The header shows an `⬡ worker` badge with the
   agent's tailnet address and cluster.
4. Output streams back through the mesh into xterm. Stats tab pulls
   per-process metrics from the agent pod.
5. To inspect an agent independently: Settings → Agents → Container
   Workers → click the agent row. Detail page shows attestation chain
   + per-pod resource usage.
6. **Terminate** the agent from its row's action menu (force-kills
   the pod; session goes to Failed if mid-run).

## Other channels

### 5a. Mobile (Compose Multiplatform)

Settings → Agents → Container Workers card. Spawn flow same as PWA.
Per-agent attestation chain renders as a vertical card list.

### 5b. REST

```sh
# Mint a fresh per-spawn token (used internally by sessions/start
# when --agent != local; exposed for debugging).
curl -sk -X POST -H "Authorization: Bearer $TOKEN" $BASE/api/agents/mint

# Spawn an agent (typically called by sessions/start; direct path:).
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"cluster":"lab-east","profile":"datawatch-dev","backend":"claude-code"}' \
  $BASE/api/agents

# List + get + attestation.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/agents
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/agents/dw-agent-7a3c
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/agents/dw-agent-7a3c/attestation

# Terminate.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  $BASE/api/agents/dw-agent-7a3c/terminate
```

### 5c. MCP

Tools: `agent_mint`, `agent_spawn`, `agent_list`, `agent_get`,
`agent_audit`, `agent_terminate`.

A coordinator LLM can spawn fan-out agents directly via MCP — useful
for parallel execution of independent tasks where each is best run
on a separate node.

### 5d. Comm channel

| Verb | Example |
|---|---|
| `agent list` | Returns active fleet. |
| `agent spawn cluster=lab-east profile=datawatch-dev "<task>"` | Spawns + returns the session ID. |
| `agent terminate <agent-id>` | Force-kills. |

### 5e. YAML

`agents.*` block in `~/.datawatch/datawatch.yaml`:

```yaml
agents:
  enabled: true
  default_driver: k8s
  image: datawatch/agent:latest
  image_pull_policy: IfNotPresent
  pqc_bootstrap_key: ${secret:PQC_BOOTSTRAP_KEY}
  default_resources:
    cpu: "2"
    memory: 4Gi
  tailscale:
    enabled: true
    control_plane_url: http://headscale.your.host:8080
    preauth_key: ${secret:TS_PREAUTH_KEY}
    hostname_prefix: dw-agent
    tags: [tag:datawatch-agent]
```

Cluster-specific overrides live in `~/.datawatch/profiles/clusters/<name>.yaml`
(see [`profiles.md`](profiles.md)).

## Diagram

```
  Operator → Daemon
                │
                │ session --agent k8s --cluster lab-east
                ▼
        Pod spawn (kubectl apply)
                │
                ▼
  ┌──────────────────────────────────────┐
  │ Pod (lab-east cluster)               │
  │  ┌──────────────┐   ┌───────────────┐│
  │  │ datawatch    │   │ Tailscale     ││
  │  │  agent       │◄──┤  sidecar       ││
  │  │  + LLM       │   │  (mesh join)  ││
  │  └──────────────┘   └───────────────┘│
  └──────────────┬───────────────────────┘
                 │ tailnet (100.x.y.z)
                 ▼
            Daemon host
            stream output
            back to operator
```

## Common pitfalls

- **Pod scheduled but agent never registers.** Check pod logs:
  `kubectl logs <pod>`. Common: PQC bootstrap key mismatch (rotated
  at host but not propagated; rotate again or restart agents).
- **Mesh not joining.** TS_PREAUTH_KEY expired / wrong control plane
  URL. See [`tailscale-mesh.md`](tailscale-mesh.md).
- **Per-pod auth fails.** Per-spawn token has a short TTL (default
  10 min); if the pod is slow to schedule, token expires before
  registration. Bump `agents.spawn_token_ttl` for slow clusters.
- **Image pull fails.** Use a pull-secret if pulling from a private
  registry; reference it via `agents.image_pull_secrets: [...]`.
- **Stale agents in the control plane.** Pods terminate but Tailscale
  node entries stick around. Run `datawatch tailscale prune` (see
  [`tailscale-mesh.md`](tailscale-mesh.md)).

## Linked references

- See also: [`tailscale-mesh.md`](tailscale-mesh.md) — mesh setup.
- See also: [`profiles.md`](profiles.md) — Cluster Profiles.
- See also: [`secrets-manager.md`](secrets-manager.md) — TS_PREAUTH_KEY +
  PQC_BOOTSTRAP_KEY storage.
- See also: [`federated-observer.md`](federated-observer.md) — agents auto-push observer stats.
- Architecture: `../architecture-overview.md` § Container workers + mesh.

## Screenshots needed (operator weekend pass)

- [ ] Container Workers card with active fleet
- [ ] New-session wizard with Agent + Cluster dropdowns
- [ ] Session detail with `⬡ worker` badge in header
- [ ] Per-agent detail page with attestation chain
- [ ] CLI `datawatch agents list` output
