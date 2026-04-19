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
| `image_prefix` | `""` | image registry+namespace (e.g. `registry.example.com/datawatch`); per-cluster `image_registry` wins |
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

## Memory federation (Sprint 6 in flight)

Each spawned worker can read + write the parent's episodic memory
under one of three modes (set per Project Profile):

| Mode | Worker reads | Worker writes | Notes |
|------|--------------|---------------|-------|
| `shared` | parent + own namespace | direct to parent | full bidirectional federation |
| `sync-back` | parent + own namespace | local sqlite, batched POST on session-end | conflict policy: append-only, timestamps win, KG triples merged |
| `ephemeral` | own namespace only | tmpfs/overlay, nothing syncs | useful for read-only audits / one-shots |

**S6.1 — namespace enforcement (shipped):** the SQLite + Postgres
memory tables gain a `namespace` column (default `__global__` to
preserve pre-F10 behaviour). New methods on `internal/memory.Store`:

**S6.2 + S6.4 — bootstrap memory bundle (shipped):**
`BootstrapResponse.Memory` now carries `{Mode, Namespace}` — the
worker reads it on startup and exports `DATAWATCH_MEMORY_MODE` +
`DATAWATCH_MEMORY_NAMESPACE` for the in-container memory client.
Mode `ephemeral` → worker uses local memory only, nothing syncs.
Mode `shared` / `sync-back` → worker uses parent's
`/api/memory/save|search|import` endpoints under the assigned
namespace (HTTP-backed adapter wiring queued as **BL100**).

**S6.7 — pgvector required-or-fallback (shipped):**
New `memory.fallback_sqlite` knob (configurable via every channel —
config.yaml, REST PUT /api/config, MCP `config_set`, comm
`configure memory.fallback_sqlite=true`, CLI via config edit).
Default false. When `memory.backend=postgres` and the connection
fails at startup AND `fallback_sqlite=true`, the daemon logs a
warning and falls back to the SQLite store at `memory.db_path`.
When false (default), Postgres failure disables memory entirely
— matches the existing strict-failure behaviour. Useful for slim
worker images that prefer "always have local memory" over "always
shared with the parent".

**S6.5 — cross-profile sharing (shipped):**
`ProjectProfile.Memory.SharedWith []string` lets a profile opt into
sharing its memory namespace with another. Datawatch enforces
**mutual opt-in** — A only sees B's memory when A.SharedWith
contains B AND B.SharedWith contains A. New helper:

```go
ProjectStore.EffectiveNamespacesFor(name string) []string
```

Returns the union of own + reciprocally-shared namespaces, in
caller order (own first). Defence: single-sided declarations are
silently dropped (no leak when one operator misconfigures their
profile to claim sharing the other never agreed to). Server-side
wiring of this into `/api/memory/search` (so a worker just passes
its `agent_id` and gets the federated namespace set automatically)
is queued as **BL101**.

**S6.3 — sync-back upload protocol (shipped):**
`ExportMemory` JSON now round-trips `wing/room/hall/namespace +
embedding` so a worker's local sqlite write surfaces in the parent's
federated search after upload. New methods:

```go
Store.ExportSince(w io.Writer, namespace string, since time.Time) error
Store.Import(r io.Reader) (int, error)   // honours per-row Namespace
```

Worker flow on session-end (BL100 wires this):
1. `ExportSince(buf, profile.Namespace, sessionStartedAt)`
2. POST buf to `parent.URL + /api/memory/import` with auth header
3. Parent's `Store.Import` deduplicates by content hash + tags rows
   with the worker's namespace; spatial metadata + embeddings flow
   through unchanged.



```go
SaveWithNamespace(ns, projectDir, content, summary, role, sessionID, wing, room, hall, embedding) (int64, error)
SearchInNamespaces([]string{"profile-foo", "__global__"}, queryVec, topK) ([]Memory, error)
```

Existing `Save` / `SaveWithMeta` callers are unchanged — they
default to `__global__` so non-F10 deployments work identically.

Sprint 6 follow-ups will wire the per-Project-Profile namespace
through the bootstrap response, the worker's memory client, and the
sync-back upload path.

## Post-quantum bootstrap tokens (S5.2)

Sprint 3 shipped the F10 spawn flow with a 32-byte hex bootstrap
token: simple, fast, but compromised the moment an attacker reads
the parent's spawn log or sniffs the worker container env.

S5.2 replaces (additively, opt-in) that single secret with a
**structured envelope authenticated by NIST-standardised
post-quantum primitives**:

- **ML-KEM 768** for key encapsulation (Cloudflare CIRCL)
- **ML-DSA 65** for signing (same)

Wire format (delivered as the `token` field on
`POST /api/agents/bootstrap`):

```
<kem-ciphertext-b64> "." <signature-b64>
```

Verify path: parent splits + base64-decodes both halves,
KEM-decapsulates the ciphertext against the keys it retained at
spawn (yields the shared secret), verifies the signature against
the worker's signing public key over the agent_id. Both must
verify; one-without-the-other is rejected. Replay rejection is
inherent — KEM encapsulation is randomised, so each token instance
is unique even for the same (agent_id, keypair) pair.

`internal/agents/pqc_token.go` exposes:

- `GeneratePQCKeys()` — fresh KEM + signing keypairs per spawn
- `MakePQCBootstrapToken(agentID, keys)` — worker side
- `VerifyPQCBootstrapToken(envelope, agentID, keys)` — parent side

Shipped as opt-in primitives; wiring into the spawn driver +
`ConsumeBootstrap` is tracked as **BL95** so deployments adopt
incrementally and the existing UUID flow keeps working until the
operator flips `agents.pqc_bootstrap=true`.

## Production hardening (Sprint 8 in flight)

**S8.5 — Cloud Foundry driver placeholder (shipped):**
`agents.CFDriver` is a typed stub returning `ErrCFNotImplemented`
on every method. The schema-level `kind: cf` Cluster Profile
acceptance has been there since Sprint 2; this commit makes
spawning against `kind=cf` fail fast with a clear error pointing
operators at S8.5 in the F10 plan rather than a generic
"no driver registered" message. Concrete impl (shell-out to `cf`
CLI) is future work.

**S8.4 — Agent audit trail (shipped):**
- `agents.AuditEvent{At, Event, AgentID, Project, Cluster, State,
  Note, Extra}` — single shape across every emission point
- `agents.Auditor` interface (Append) — implementations:
    - `MemoryAuditor` for tests + temporary inspection (Recent/All)
    - `FileAuditor` for production (JSON-lines or CEF format)
- `Manager.Auditor` field (optional) — when set, emits on
  `spawn` / `spawn_fail` / `result` / `terminate`
- **Two output formats** per [AGENT.md → Audit Logging Rule](../AGENT.md):
    1. JSON-lines (`FormatJSONLines`) — `jq`-friendly default for in-
       house pipelines + the datawatch web UI
    2. CEF (`FormatCEF`) — ArcSight Common Event Format for SIEM
       forwarding (Splunk, QRadar, ArcSight, Sentinel, Chronicle).
       SignatureID + severity assignments inline in
       `internal/agents/audit.go`'s `cefSignature` table.
- `FormatCEFLine(ev)` produces the SIEM-ready single-line form
  with proper header (`|` + `\`) and extension (`=` + `\` + `\n` +
  `\r`) escaping per the CEF spec — escape coverage is treated as
  security-critical (bad escapes break SIEM parsing or let an
  attacker inject synthetic events)
- REST surface (GET `/api/agents/audit` + filters) wires as
  **BL107**

Sibling system: `internal/auth.TokenBroker` already has its own
JSON-lines audit (S5.1) covering token mint/revoke/sweep — same
shape, mirrored CEF support tracked as a follow-up.

**S8.1 — Secrets pluggability (shipped):**
New `internal/secrets` package with `Provider` interface (Get/Put)
+ five backends:

- `File` (default) — 0600 atomic file under `~/.datawatch/secrets/`
  (operator-overridable). Path-traversal rejected. Trims trailing
  newlines so editor-saved files Just Work.
- `EnvVar` — process env, prefix-able. Empty value = `ErrNotFound`
  (security hardening — empty secret is never valid). Slash-keys
  rejected.
- `K8sSecret`, `Vault`, `CSI` — typed stubs returning
  `ErrNotImplemented` with the operator-requested kind in the
  error so operators see exactly which backend is missing.

`secrets.Resolve(kind, baseDir)` dispatches to the matching impl;
unknown kind returns the stub-with-named-kind. Wiring into
`ClusterProfile.CredsRef` resolution + token broker storage is
queued as **BL111** so operators can back the existing
`creds_ref.provider` field with whichever backend matches their
infrastructure (Vault for prod, File for dev, EnvVar for K8s when
secrets are projected as env).

**S8.3 — Multi-cluster (shipped):**
New `ProjectProfile.DefaultClusterProfile` lets a project pin its
preferred cluster so spawn requests can omit `cluster_profile` and
fall back to the default. Explicit `cluster_profile` on the spawn
request always wins (operator override). Empty default + no
override produces a clear error pointing at the field. Operators
manage multiple clusters by creating one `ClusterProfile` per
(gcp/aws/on-prem/local); kubectl context caching is already
handled by the K8s driver's shell-out semantics.

**S8.6 — Idle-timeout enforcement (shipped):**
`ProjectProfile.IdleTimeout` (already in schema since Sprint 2)
finally has runtime teeth. New `Manager.NoteActivity(agentID)`
bumps a per-agent `LastActivityAt` timestamp; new
`Manager.ReapIdle(ctx, now)` walks active agents and `Terminate()`s
any whose profile timeout has elapsed since last activity (falls
back to `CreatedAt` floor when no activity recorded). Reap emits
an `idle_reap` audit event with the actual idle duration. Per-call
`now` parameter keeps the reaper deterministic for tests; the
production sweeper goroutine wiring is **BL108**. `IdleTimeout=0`
disables the policy entirely.

`ConsumeBootstrap` + `RecordResult` both bump `LastActivityAt` as
sure-fire activity signals. BL108 will thread the bump into the
agent reverse proxy / memory proxy / peer broker / MCP call paths
so every real worker→parent interaction counts as activity.

**S8.7 — Crash policy field (shipped):**
New `ProjectProfile.OnCrash` enum (default = empty = `fail_parent`).
Three values:

- `fail_parent` (default + safest) — surface the failure to the
  parent session, do not respawn
- `respawn_once` — single retry with the same task; further failures
  revert to fail_parent semantics
- `respawn_with_backoff` — exponential backoff (1m, 2m, 4m, …,
  capped at 30m) until manual operator intervention

Validation enforced at profile load + via every channel. Runtime
enforcement (Manager loop reacting to Failed transitions + retry
state) is queued as **BL106**.

**BL102 — Worker comm-channel proxy-send (shipped):**
S7.7 shipped the bootstrap `Comm.Channels` list + `DATAWATCH_COMM_INHERIT`
env so workers know which channels are active on the parent. BL102
wires the parent-side endpoint they POST to:

- `POST /api/proxy/comm/{channel}/send` body `{recipient?, message}`
  — recipient defaults to the parent's per-channel
  default (group ID, chat ID, room) when omitted; the parent looks
  the channel name up in the registry installed at startup and calls
  `backend.Send(recipient, message)`.

Status codes:
- 200 + `{status: "sent", channel, backend}` on success
- 404 when channel isn't active on this parent
- 503 when no comm backends were registered
- 502 when the backend's Send returns an error
- 400 on missing message / no recipient + no default

main.go now declares `commBackends := map[string]messaging.Backend{}`
populated inside `newRouter` (one entry per backend constructed) and
hands the map to the server via `SetCommBackends`. Worker-side
outbound helper (the env+retry shape) lands alongside BL100.

**BL104 — Peer broker REST proxy (shipped):**
S7.6 shipped the in-process `PeerBroker`. BL104 wires the parent-
side REST surface so workers can talk to each other without bypassing
the parent's trust boundary:

- `POST /api/agents/peer/send` body `{from, to[], topic, body}` —
  delivers to each recipient's per-agent inbox; sender authorization
  (`AllowPeerMessaging` on the sender's profile) enforced inside
  `broker.Send`. Returns `{delivered, dropped[]}`.
- `GET /api/agents/peer/inbox?id=<recipient>&peek=0|1` — returns
  the recipient's queue; `peek=0` (default) drains, `peek=1` is
  non-destructive.

The broker is wired in main with `agentMgr` + the default 100-message
inbox cap. Worker-side outbound helper lands alongside BL100 (the
HTTP memory client uses the same retry/auth shape).

**BL107 — Agent audit trail query (shipped):**
S8.4 shipped the `Auditor` interface + Manager emissions. BL107
wires the read path. Operators set `agents.audit_path` (default
`<data_dir>/audit/agents.jsonl`; `-` disables) and optional
`agents.audit_format_cef` (default false → JSON-lines so the REST
query handler can read it back). The new
`agents.ReadEvents(path, filter, limit)` parses JSON-lines; CEF
files are off-limits (operators query their SIEM instead). Surfaces
per the every-channel rule:

- REST: `GET /api/agents/audit?event=&agent_id=&project=&limit=`
- MCP: `agent_audit` tool (same params)
- comm: `agent audit [<agent-id>]` — pretty-printed last 20
- CLI follow-up: deferred (operator can curl the REST endpoint)

A Settings UI "Agents card" surface remains a UI follow-up; the
data path is in place.

**BL101 — Cross-profile memory namespace expansion (shipped):**
S6.5 shipped `ProjectStore.EffectiveNamespacesFor` returning the
mutual-opt-in namespace union for a given profile. BL101 wires it
into `GET /api/memory/search`: when the caller passes
`?profile=<name>` (or `?agent_id=<id>` — the agent's profile is
looked up via `agentMgr.Get`), the handler resolves the effective
namespace set and calls a new `MemoryAPI.SearchInNamespaces` instead
of plain `Search`. The underlying `Retriever.RecallInNamespaces`
embeds the query and uses a new optional `NamespacedBackend`
capability interface (SQLite `Store` implements; the Postgres path
returns `ErrNamespaceUnsupported` until the matching pgvector query
lands). Workers can query peer-shared memory without knowing the
peer's raw namespace string.

**BL106 — Runtime OnCrash enforcement (shipped):**
S8.7 shipped the `OnCrash` field + validation. BL106 wires the
runtime response: `Manager.HandleCrash(ctx, agent)` is called on the
spawn-failure path and dispatches to either `respawnOnce` (single
retry honoured per-(project, branch, parent) tuple) or
`respawnWithBackoff` (immediate first retry; subsequent crashes
deferred 1m → 2m → 4m → … capped at 30m). `Manager.ResetCrashRetries`
clears the per-key counter when an operator manually intervenes.
Crash detection from polled state transitions (Failed observed
outside of Spawn) is the BL112-adjacent reconciler that walks
labelled containers/Pods.

**BL95 — PQC bootstrap envelope wiring (shipped):**
S5.2 shipped the PQC primitives (ML-KEM 768 + ML-DSA 65) as opt-in
building blocks. BL95 wires them into the spawn → bootstrap path:
operators set `agents.pqc_bootstrap: true`; `Manager.Spawn` mints a
fresh keypair into `Agent.PQCKeys`; the Docker + K8s drivers inject
`DATAWATCH_PQC_MODE`/`_KEM_PRIV`/`_KEM_PUB`/`_SIGN_PRIV` env vars;
`Manager.ConsumeBootstrap` accepts a PQC envelope (`<b64-ct>.<b64-sig>`)
or the legacy UUID side-by-side, so partial rollouts (PQC-aware
parent + older worker image) keep bootstrapping. Both forms are
single-use; the keys are zeroed on consume alongside the UUID.

**BL108 — Idle-reaper sweeper goroutine (shipped):**
S8.6 shipped the `Manager.NoteActivity` + `ReapIdle` primitives.
BL108 wires the periodic loop: `Manager.RunIdleReaper(ctx, interval)`
launches a background goroutine that calls `ReapIdle` on the
configured cadence (default 60s, clamped to 10s minimum). Operators
control the cadence via `agents.idle_reaper_interval_seconds`
(default 0 = 60s; negative = disabled). The reaper short-circuits
when no Project Profile sets a non-zero `idle_timeout`, so the cost
in profiles that don't opt in is a single map walk per tick.

**BL92 / BL93 / BL94 — Orphan session reconciliation (shipped):**
Three small backlog items shipped together because they address one
problem: session directories on disk that don't appear in the
`sessions.json` registry (observed with session `cdbb`).

- **BL92 — Write-through registry**: `Store.Save` already flushed
  synchronously; added explicit `Flush()` API for external reconcilers
  + `TestStore_Save_WriteThrough` regression test pinning the contract.
- **BL93 — Startup reconciler**: new `Manager.ReconcileSessions(autoImport)`
  walks `<data_dir>/sessions/<id>/session.json` and reports orphans.
  Main daemon runs it on boot, gated by new `session.reconcile_on_startup`
  config knob (default false → dry-run + log; opt-in for auto-import).
- **BL94 — Manual import**: `Manager.ImportSessionDir(dir)` is the
  single-dir entry point.

Per the every-channel rule, both BL93 and BL94 are exposed via REST
(`POST /api/sessions/reconcile`, `POST /api/sessions/import`), MCP
(`session_reconcile`, `session_import`), CLI (`datawatch session
reconcile [--apply]`, `datawatch session import <dir-or-id>`), and
comm channels (`session reconcile [apply]`, `session import <dir|id>`).

**S8.2 — Service-mode workers (shipped):**
New `ProjectProfile.Mode` enum: `ephemeral` (default) or `service`.
Ephemeral workers terminate when their session ends or after the
S8.6 idle window. Service workers are long-lived: the idle reaper
in `Manager.ReapIdle` walks every active agent and short-circuits
when `project.Mode == "service"`, leaving them running until an
operator issues an explicit terminate. Validation is case-sensitive
and enforced at profile load + via every channel. Reattaching
service workers after a parent restart (walking labelled containers/
Pods + reconstructing the in-memory record) is queued as **BL112**.

## Pointing datawatch at *your* registry / cluster / git account

Datawatch ships zero hard-coded production hosts — every registry,
kubectl context, and git credential is operator-configured. See
[**docs/registry-and-secrets.md**](registry-and-secrets.md) for the
full walkthrough: build-time `REGISTRY` overrides, `agents.image_prefix`
config knob (settable via REST/MCP/CLI/comm/YAML), per-cluster
`image_registry` override, Helm chart `image.registry` (no default —
must be set), kubectl context resolution (`~/.kube/config` or in-
cluster ServiceAccount), `gh auth login` for git tokens, secrets
handling (gitignored files + Helm Secret patterns + SealedSecret
recommendation), and audit recipes to verify nothing leaks.

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

## Multi-agent orchestration (Sprint 7 in flight)

Sprint 7 turns spawned workers from "one-shot helpers" into a
coordinated orchestration layer. Stories shipped so far:

**S7.3 — workspace lock (shipped):** `Manager.Spawn` rejects a
request that would put two active workers on the same
`(project_profile, branch)` tuple. Concurrent workers on the same
workspace would race each other's git pushes — clean refusal at
spawn time is the cheapest fix. Branch defaults to the profile's
`git.branch` when not supplied; pass `branch` on the spawn
request for distinct workspaces. Lock auto-releases on Terminate
(Stopped/Failed agents are excluded from the lock check).

**S7.1 — orchestrator core (shipped):**
`agents.Orchestrator.Run(ctx, plan)` walks an `OrchestratorPlan` of
`OrchestratorNode{ID, ProjectProfile, ClusterProfile, Task,
Branch, DependsOn[]}`. Nodes spawn when every upstream node reaches
`NodeDone`; downstream cascades to `NodeSkipped` on failure.
Concurrency capped at `MaxConcurrent` (default 4) on top of the
existing per-cluster workspace lock + per-parent recursion budget.
Cycle detection runs up-front via DFS; missing deps + duplicate
IDs + missing required fields all rejected before any spawn fires.
Returns a `PlanRun{Nodes: map[id]*NodeRun{State, AgentID, Result,
Error, StartedAt, FinishedAt}, StartedAt, FinishedAt}` for caller
inspection. F15 `pipelines.Executor` wiring is **BL105**.

**S7.2 — fan-in result aggregation (shipped):**
New endpoint `POST /api/agents/{id}/result` accepts a structured
`AgentResult{Status, Summary, Artifacts}`. Stored on the Agent
record; visible via `GET /api/agents/{id}` JSON. The orchestrator
reads `Result.Status == "ok"` to advance the DAG; non-ok / no
report after agent terminate cascades downstream as `NodeFailed`.

**S7.6 — peer-to-peer messaging (shipped):**
New `ProjectProfile.AllowPeerMessaging bool` (default false; opt-in
per profile via every channel). New `agents.PeerBroker` provides
in-process per-recipient inboxes with capacity caps + sender-profile
authorisation:

```go
broker := NewPeerBroker(agentMgr, 100)         // maxInbox=100
delivered, dropped, err := broker.Send(senderID, []string{recipID}, "topic", "body")
inbox := broker.Drain(recipID)                  // pulls + clears
snap  := broker.Peek(recipID)                   // pulls without clearing
```

Workers don't talk to each other directly — every message routes
through the parent's broker, which:
- enforces sender's `AllowPeerMessaging` (rejects when false)
- skips recipients whose agent is unknown or in
  Stopped/Failed state (returned in `dropped` list for partial-
  fanout handling)
- bounds each recipient's queue at `maxInbox` (overflow drops with
  the rest)
- snapshots sender's profile at spawn time — operator can't yank
  P2P out from under a running worker

The HTTP/WS surface (`POST /api/agents/{from}/peer/send` +
`GET /api/proxy/agent/{id}/peer/inbox`) + worker-side outbound
helper are queued as **BL104**. The broker is also the foundation
for Sprint 7's orchestrator (S7.1) — the orchestrator agent uses
this same routing layer to address its workers.

**S7.5 — validation orchestrator trigger (shipped):**
New `ProjectProfile.AutoValidate bool` (default false) +
`ValidateProfile string` (default "validator"). When a worker
session reaches a terminal state and AutoValidate is true, the
parent spawns a small read-only validator agent against the named
profile, on the same Cluster Profile as the worker. Branch is
auto-derived (`validate-{agent[:8]}-{unix}`) so it never collides
with the worker's branch under the workspace lock. The validator's
actual image + check logic (PR diff sanity, declared-task vs
observed work, memory writes) is queued as **BL103** — this commit
ships only the trigger.

**S7.7 — comm-channel inheritance (shipped):**
New `ProjectProfile.CommInheritance []string` lists parent
messaging-backend names the worker should route alerts through
(e.g. `["signal", "telegram"]`). The bootstrap response carries the
list under `BootstrapResponse.Comm.Channels`; `ApplyBootstrapEnv`
exports it as `DATAWATCH_COMM_INHERIT` (CSV) for the worker
daemon's outbound alert path. Empty list = no inheritance (worker's
alerts stay local). The actual proxy-send endpoint
(`/api/proxy/comm/{channel}/send`) + worker outbound wiring is
queued as **BL102** so credentials never leave the parent.

**S7.4 — recursion gates (shipped):** when `SpawnRequest.ParentAgentID`
is set (recursive child spawn from a worker), the Manager enforces
the parent agent's profile budgets:

- `allow_spawn_children` must be `true` (default false — explicit
  opt-in)
- `spawn_budget_total` caps lifetime children per parent (0 = no cap)
- `spawn_budget_per_minute` caps recent children per parent (0 = no cap)

All three fields live on the existing `ProjectProfile` (S2 schema)
and are configurable via every channel. Top-level operator-initiated
spawns (no `ParentAgentID`) bypass the recursion gates entirely —
gates apply only to recursive children.

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
