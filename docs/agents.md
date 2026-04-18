# Ephemeral Agents

F10 Sprint 3 introduces ephemeral container-spawned agent workers.
A "session" can optionally run inside a dedicated container instead of
on the parent host — the parent stays the control plane, the container
does the work, the container dies when the work is done.

Every operator-facing knob and every agent operation is reachable via
**REST, MCP, CLI, comm channels, and the Web UI**, per project rules.

## The shape

```
┌──────────────────────┐        ┌─────────────────────────────────────┐
│   Parent datawatch    │──spawn→│ Worker container                    │
│                      │        │   datawatch start --foreground       │
│   Manager tracks      │◀──hit──│   (agent-claude image, built in     │
│   lifecycle:          │  /api/ │    Sprint 1)                         │
│   pending → starting  │  agents│                                      │
│   → ready → running   │   /    │   bootstraps once, runs task, exits │
│   → stopped           │  boots │                                      │
└──────────────────────┘        └─────────────────────────────────────┘
```

A worker's Pod / container is derived from two inputs:

* **Project Profile** — what repo, which agent (claude/opencode/etc),
  which language sidecar, memory policy
* **Cluster Profile** — where to run (docker / k8s), registry, namespace,
  trusted CAs

Both were added in Sprint 2; see [profiles.md](profiles.md).

## Spawn flow

1. Operator (CLI / chat / MCP / UI) submits a spawn request naming a
   project + cluster profile + optional task description
2. Manager resolves both profiles, validates them, mints a 32-byte hex
   single-use bootstrap token, picks the driver for the cluster kind
3. Driver (Docker + K8s registered; CF stubbed) creates
   the container with these env vars:
    - `DATAWATCH_BOOTSTRAP_URL` — where to call home
    - `DATAWATCH_BOOTSTRAP_TOKEN` — the single-use token
    - `DATAWATCH_AGENT_ID` — its own UUID
    - `DATAWATCH_TASK` — the operator's task string
4. Worker (`datawatch start --foreground` inside the container)
   detects the bootstrap env on startup, calls
   `POST /api/agents/bootstrap` with `{agent_id, token}` (retries any
   transport error or 5xx with exponential backoff up to 60s; 4xx is
   terminal — exits non-zero so the container restarts)
5. Parent's Manager burns the token, returns the worker's effective
   config (profile contents, eventual git token, memory URL)
6. Worker starts its own `datawatch start --foreground`, reaches
   `/readyz=200`, transitions to State=ready
7. Session bound → State=running; work proceeds
8. On session end, Manager calls driver.Terminate; container removed

## Configuration (`agents:` block in config.yaml)

| key | default | purpose |
|---|---|---|
| `image_prefix` | `""` | image registry+namespace (e.g. `harbor.dmzs.com/datawatch`); per-cluster `image_registry` wins |
| `image_tag` | `v$(Version)` | image tag; override to pin workers to a specific release |
| `docker_bin` | `docker` | binary the Docker driver shells out to; set `podman` for rootless |
| `kubectl_bin` | `kubectl` | binary the K8s driver shells out to; set `oc` for OpenShift |
| `callback_url` | (resolved at boot) | URL workers dial for bootstrap. Resolution: `agents.callback_url` → `server.public_url` → `http://<bind-host>:<port>`. **K8s caveat:** Pods cannot reach `0.0.0.0` — set `server.public_url` (or `agents.callback_url`) to a routable LAN/cluster URL when running k8s spawns. |
| `bootstrap_token_ttl_seconds` | `300` | how long a minted token stays valid |
| `worker_bootstrap_deadline_seconds` | `60` | total wall-clock budget the worker has to complete its bootstrap call before exiting (slow networks may need longer); injected into the spawned container as `DATAWATCH_BOOTSTRAP_DEADLINE_SECONDS` |

Reachable via every channel per rules:

* **config.yaml** — `agents:` block
* **REST** — `GET /api/config` shows `agents: {...}`; `PUT /api/config` with `{"agents.image_prefix":"…"}`
* **MCP** — `config_set key=agents.image_prefix value=…`
* **CLI** — edit config.yaml (uniform with other sections; no dedicated `datawatch config set`)
* **Comm** — `configure agents.image_prefix=…`

## Agent operations

### REST

```
POST   /api/agents                {"project_profile","cluster_profile","task"}
GET    /api/agents
GET    /api/agents/{id}
GET    /api/agents/{id}/logs?lines=N
DELETE /api/agents/{id}
POST   /api/agents/bootstrap      (unauthenticated; worker-only)

# S3.5 — reverse proxy to a worker's HTTP/WS API
ANY    /api/proxy/agent/{id}/...  (forwards to http://<ContainerAddr>/...
                                   WebSocket Upgrade auto-detected and
                                   bidirectionally relayed; client headers
                                   forwarded; 404 if id unknown, 503 if
                                   worker has no reachable address yet)

# S3.6 — bind a session to a worker agent
POST   /api/sessions/bind         {"id":"<session>","agent_id":"<agent>"}
                                  (empty agent_id unbinds; reads of a
                                   bound session's output now transparently
                                   forward through the proxy above)
```

Status codes mirror the profile endpoints (201/200/204/404/400/503).
Bootstrap failures return 401 on token/state mismatch.

### MCP

```
agent_spawn          project_profile, cluster_profile, [task]
agent_list
agent_get            id
agent_logs           id, [lines]
agent_terminate      id
session_bind_agent   session_id, [agent_id]     # empty agent_id unbinds
```

All proxy to the local REST API. `agent_spawn` returns the full Agent
record (bootstrap token always redacted).

### CLI

```
datawatch agent spawn --project <name> --cluster <name> [--task "..."]
datawatch agent list [-f table|json|yaml]
datawatch agent show <id> [-f json|yaml]
datawatch agent logs <id> [-n lines]
datawatch agent kill <id>
datawatch session bind <session-id> <agent-id>   # F10 S3.6 — empty <agent-id> unbinds
```

Requires daemon running. Uses REST under the hood.

### Comm channels (signal/telegram/discord/slack/matrix/webhook)

```
agent list
agent spawn <project> <cluster> [<task>]
agent show <id>
agent logs <id>
agent kill <id>
bind <session-id> <agent-id>     # F10 S3.6; "-" in place of <agent-id> unbinds
```

Unlike profile writes (deliberately chat-blocked), agent spawn IS
available over chat — the blast radius of a misspelled agent spawn
is one wasted container, not a corrupted profile.

### Web UI

Sprint 4 deliverable. The Settings → General page will gain an
**Agents** card showing live workers; the session card will gain a
worker badge once a session binds to an agent.

## End-to-end smoke

`tests/integration/spawn_docker.sh` walks the Sprint 3 REST flow
against a running parent daemon: profile create → agent spawn →
agent get → bootstrap token validation → agent terminate → profile
cleanup. Default image is `busybox:latest` (placeholder — the
container just needs to exist for Terminate to reap). Set
`RUN_BOOTSTRAP=1` with a real worker image to also assert
`state=ready` after bootstrap completes.

```
tests/integration/spawn_docker.sh [BASE_URL]
```

## Security notes

* **Bootstrap token** — 32-byte hex, minted at spawn, never logged,
  never emitted in JSON snapshots (json:"-" + `cloneAgent` zeros it),
  burned on first consume. Sprint 5 wraps it in post-quantum
  signature/encryption.
* **Container labels** — every spawned container gets
  `datawatch.role=agent-worker` + `datawatch.agent_id=<id>` so the
  Sprint 7 reconciler can find our workers even if in-memory state
  is lost (e.g. parent daemon restart).
* **Image prefix / pull secret** — per-cluster override on
  `ClusterProfile.image_registry` + `image_pull_secret` lets the same
  Project Profile pull from harbor in prod and localhost:5000 in dev.
* **TLS trust** — Sprint 4's `ClusterProfile.trusted_cas[]` field
  projects PEM blobs into worker Pods so they trust private CAs
  for registry + callback + memory connections. **Until Sprint 4
  lands, the worker's bootstrap client uses `InsecureSkipVerify: true`
  by design** so dev parents on self-signed certs Just Work — a
  documented Sprint 3 scope decision, not a violation. Once
  `trusted_cas[]` is in, this becomes opt-in dev-only.

## Known gaps (Sprint 3 scope)

* S3.6 partial — binding metadata + read-forward for /api/output
  land in this commit; full session-write forwarding (kill, send,
  response, state) deferred to Sprint 4 alongside K8s and the
  worker UI badge. The session model has `agent_id` so the UI can
  already surface a badge based on the field.
* PQC token upgrade deferred to Sprint 5
