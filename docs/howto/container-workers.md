# How-to: Container workers

Spawn workers as Docker containers (or Kubernetes Pods) that run a
single task in isolation, then tear themselves down when done. Each
worker auto-peers with the parent's federated observer so the
operator sees per-agent CPU / RAM / GPU / net live.

## Base requirements

- Daemon up; `agents.enabled: true` in your config.
- A Docker socket reachable from the daemon (or a kubeconfig pointed
  at your cluster, for the k8s driver).
- Pre-built worker images (`agent-claude`, `agent-opencode`,
  `agent-aider`, etc.). The release pipeline pushes these to
  `ghcr.io/dmz006/datawatch-*:<version>` on every tag — pull them
  in directly, or run `make container-agent-claude` etc. for local
  builds.
- One **Project Profile** (declares the repo + branch + git auth) +
  one **Cluster Profile** (declares which driver + image + env).

## Setup

### 1. Profiles

Both profile sets live in the PWA at Settings → General → Project
Profiles / Cluster Profiles, or as YAML / REST.

```bash
datawatch profile project add --name auth-service \
  --repo https://github.com/me/auth-service \
  --branch main --auto-pr true

datawatch profile cluster add --name local-docker \
  --driver docker --image ghcr.io/dmz006/datawatch-agent-claude:latest \
  --memory 4Gi --cpu 2
```

### 2. Federation token (optional but recommended)

Workers register as Shape A peers using a bearer minted at spawn.
Auto-injected — no operator action needed once
`observer.peers.allow_register: true`.

### 3. Spawn a worker

```bash
datawatch agent spawn --project auth-service --cluster local-docker \
  --task "fix the broken test in internal/auth/middleware_test.go"
#  → {"id":"agt_a1b2", "state":"starting", …}
```

The same call from chat:

```
agent spawn auth-service local-docker fix the broken test in …
```

### 4. Watch it work

The worker boots, hits `POST /api/agents/bootstrap` to receive its
task + git token, clones the repo, starts a session. Every tick the
worker pushes a StatsResponse-v2 envelope to the parent's federated
observer.

```bash
datawatch agent show agt_a1b2
datawatch agent logs agt_a1b2
datawatch observer peer stats agt_a1b2   # live CPU / RSS / net / GPU
```

In the PWA: Settings → Monitor → Federated peers → click the agent
to drill into its envelope tree.

### 5. Validator attestation (optional)

If your Cluster Profile names a `validator_image`, the worker's
output is attested by an independent validator session before the
worker terminates. Verdict lands in the same `/api/agents/{id}`
JSON.

### 6. Termination + cleanup

```bash
datawatch agent kill agt_a1b2
```

Or let the worker exit on its own — the parent's sweeper revokes
the worker's git token within 5 min of the container disappearing,
even if it crashed without sending an explicit terminate.

## Expected operator experience

- **PWA**: a new agent row appears in Settings → Monitor → Federated
  peers within ~2-5 s of spawn; envelope tree fills as the worker
  runs the task. The Settings → Monitor card surfaces the daemon's
  view of every active envelope:

  ![Settings → Monitor — System Statistics + envelope summary](screenshots/settings-monitor.png)
- **CLI / chat**: `agent show <id>` returns state, last activity,
  resource usage, and (if the validator ran) the verdict.
- **GitHub**: when the Project Profile has `auto_pr: true`, the
  worker's branch is pushed and a PR opened *before* the parent
  revokes the git token.
- **Memory**: every worker has its own diary wing
  (`agent-<id>`) — see [How-to: Cross-agent memory](cross-agent-memory.md)
  for how to share diaries across workers.

## Reachability across channels

| Channel | Action | Command |
|---------|--------|---------|
| CLI | spawn | `datawatch agent spawn --project … --cluster … --task …` |
| CLI | observe | `datawatch agent show <id>` / `datawatch observer peer stats <id>` |
| REST | spawn | `POST /api/agents {"project":…, "cluster":…, "task":…}` |
| REST | observe | `GET /api/agents/<id>` / `GET /api/observer/peers/<id>/stats` |
| MCP | spawn | tool `agents_spawn` |
| MCP | observe | `agent_logs`, `observer_agent_stats` |
| Chat | spawn | `agent spawn <project> <cluster> <task>` |
| Chat | observe | `agent show <id>` / `agent logs <id>` |
| PWA | all | Settings → General → Agents (spawn) + Settings → Monitor → Federated peers (observe) |

## See also

- [`docs/agents.md`](../agents.md) — architecture + lifecycle deep dive
- [`docs/api/sessions.md`](../api/sessions.md) — sessions spawned by the worker
- [`docs/flow/agent-spawn-flow.md`](../flow/agent-spawn-flow.md) — Mermaid sequence
- [`docs/api/observer.md`](../api/observer.md) — federated observer reference
- [How-to: Cross-agent memory](cross-agent-memory.md) — share diaries across spawned workers
- [How-to: PRD-DAG orchestrator](prd-dag-orchestrator.md) — orchestrator-driven worker fan-out
