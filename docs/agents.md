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
    - `DATAWATCH_BOOTSTRAP_DEADLINE_SECONDS` — operator-tunable (S3.4)
    - `DATAWATCH_PARENT_CERT_FINGERPRINT` — TLS pin (S4.3, when TLS enabled)
   And — when the project has a Git URL and a TokenBroker is wired —
   the parent mints a short-lived git token (S5.1) for this worker
   that rides home in the bootstrap response (NOT in the spawn env).
4. Worker (`datawatch start --foreground` inside the container)
   detects the bootstrap env on startup, calls
   `POST /api/agents/bootstrap` with `{agent_id, token}` (retries any
   transport error or 5xx with exponential backoff up to 60s; 4xx is
   terminal — exits non-zero so the container restarts)
5. Parent's Manager burns the bootstrap token and returns the
   worker's effective config: project + cluster names, task, env
   bag, and (S5.3) the Git bundle (`git.url`, `git.branch`,
   `git.token`, `git.provider`)
6. Worker (S5.3) clones the repo into `/workspace/<repo>` using
   the token, scrubs the credential URL from `.git/config` after
   the clone, then starts its own `datawatch start --foreground`,
   reaches `/readyz=200`, transitions to State=ready
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
GET    /api/agents/ca.pem         (S4.3 — serves the parent's TLS cert
                                   PEM for ConfigMap projection /
                                   manual operator setup; 404 when TLS
                                   is disabled)

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

## Git provider + token broker (S5.1, S5.6)

Sprint 5 introduces an abstraction over git forges so workers can
clone, push, and open PRs with parent-minted, short-lived tokens
that get revoked on session end.

**Provider interface** (`internal/git/provider.go`):
```
Kind()                                            // "github" | "gitlab"
MintToken(ctx, repo, ttl)  → MintedToken          // short-lived
RevokeToken(ctx, token)                            // best-effort
OpenPR(ctx, PROptions)     → URL                  // pr/mr create
```

**Implementations**:
- `GitHub` — shells out to `gh` CLI. v1 uses the operator's existing
  PAT (`gh auth token`); fine-grained per-spawn tokens via
  `gh api …/installations/.../access_tokens` is a follow-up.
- `GitLab` — stub returning `ErrNotImplemented` until a GitLab repo
  enters the rotation. Schema-level acceptance is already in
  `ProjectProfile.Git.Provider`.

**Token broker** (`internal/auth/token_broker.go`):

```
broker := &auth.TokenBroker{Provider: git.Resolve("github"), Store: …, Audit: …}
rec, _ := broker.MintForWorker(ctx, workerID, "owner/repo", 1*time.Hour)
//  …worker uses rec.Token to clone + push…
broker.RevokeForWorker(ctx, workerID)
broker.SweepOrphans(ctx, agentMgr.ActiveIDs())   // periodic safety net
```

- TTL is capped at `MaxTTL` (default 1h)
- At most one active token per `WorkerID` — a new mint supersedes
  the prior with a best-effort revoke
- `SweepOrphans` removes records whose worker is gone OR whose
  expiry has passed; safe to call concurrently
- `RunSweeper` (S5.5) is the periodic safety net: started as a
  goroutine at daemon boot, runs one sweep immediately to clean up
  anything the previous instance leaked, then ticks every 5 min
  using `agentMgr.ActiveIDs` as the alive-worker set
- `PostSessionPRHook` (S5.4) — when a session bound to an agent
  reaches a terminal state and the project's `git.auto_pr` is
  `true`, the parent pushes the worker's working branch back to
  the project repo (token injected into URL, scrubbed after) and
  opens a PR via the configured `git.Provider`. Best-effort: any
  failure (no token, push denied, OpenPR rate-limited) is logged
  but does not block the session-end callback chain
- Audit log is JSON-per-line for `jq` inspection; every mint /
  revoke / sweep / mint-fail is recorded with worker_id, repo,
  provider, and a short note

## Helm chart for the parent (S4.4)

`charts/datawatch/` packages the datawatch parent for in-cluster
deploys. Quickstart:

```sh
helm install dw ./charts/datawatch -n datawatch --create-namespace \
  --set image.tag=v2.4.5 \
  --set publicURL=http://datawatch.datawatch.svc.cluster.local:8080
```

The chart provisions a Deployment (replicas pinned to 1 for v1), a
ClusterIP Service, a ConfigMap for `config.yaml`, optional Secret
for `apiToken`/`postgres.url`/TLS, optional PVC for state, and a
namespace-scoped Role + RoleBinding so the parent's K8sDriver can
create + delete worker Pods in its own namespace. See
`charts/datawatch/README.md` for the full values reference.

## End-to-end smoke

Two parallel scripts exercise the Docker and K8s drivers against a
running parent daemon. Both walk the same REST chain: profile
create → agent spawn → agent get → bootstrap token validation →
agent terminate → profile cleanup. Default image is
`busybox:latest` (placeholder — the container/Pod just needs to
exist for Terminate to reap). Set `RUN_BOOTSTRAP=1` with a real
worker image to also assert `state=ready` after bootstrap.

```
tests/integration/spawn_docker.sh [BASE_URL]   # kind=docker
tests/integration/spawn_k8s.sh    [BASE_URL]   # kind=k8s — honours
                                                 KUBE_CONTEXT + NAMESPACE
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
* **TLS pin (S4.3)** — when the parent has TLS enabled, it computes
  the SHA-256 fingerprint of its leaf cert at startup and injects it
  into every spawned container as `DATAWATCH_PARENT_CERT_FINGERPRINT`.
  The worker's bootstrap client pins to that fingerprint and refuses
  any other cert (no fallback to a system trust store, no TOFU). The
  parent's cert is also served at `GET /api/agents/ca.pem` for
  operator setup and `ClusterProfile.trusted_cas[]` ConfigMap
  projection. When the parent has no TLS cert configured, workers
  fall back to `InsecureSkipVerify` — explicit dev/legacy mode,
  logged as a warning.

## Known gaps (Sprint 3 scope)

* S3.6 partial — binding metadata + read-forward for /api/output
  land in this commit; full session-write forwarding (kill, send,
  response, state) deferred to Sprint 4 alongside K8s and the
  worker UI badge. The session model has `agent_id` so the UI can
  already surface a badge based on the field.
* PQC token upgrade deferred to Sprint 5
