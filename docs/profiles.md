# Project and Cluster Profiles

Introduced in F10 Sprint 2. A session is determined by two profiles:

- **Project Profile** — *what* to do. Picks the git repo, agent + sidecar
  images, memory policy, spawn budgets, per-project env.
- **Cluster Profile** — *where* to run. Docker or Kubernetes, context,
  namespace, registry, trusted CAs, creds reference.

The pair is orthogonal. A single Project Profile can be spawned on many
Cluster Profiles (dev laptop, staging k8s, prod k8s); a single Cluster
Profile hosts many Projects.

Both profiles are reachable via **REST, MCP, CLI, comm channels, and the
Web UI** — the project rules require every setting to be configurable
through every channel. Storage is JSON at `~/.datawatch/profiles/*.json`,
transparently encrypted at rest when the daemon is started with `--secure`.

## Quick start

```yaml
# project-datawatch-app.yaml
name: datawatch-app
description: Mobile companion (KMP) for dmz006/datawatch
git:
  url: https://github.com/dmz006/datawatch-app
  branch: main
  provider: github
image_pair:
  agent: agent-claude
  sidecar: lang-kotlin
memory:
  mode: sync-back
```

```yaml
# cluster-testing.yaml
name: testing
kind: k8s
context: testing
namespace: datawatch-agents
image_registry: registry.example.com/datawatch
```

```bash
datawatch profile project create -f project-datawatch-app.yaml
datawatch profile cluster create -f cluster-testing.yaml
datawatch profile project smoke datawatch-app
datawatch profile cluster smoke testing
```

In Sprint 3+ a session spawn will take both names:
```bash
datawatch agent spawn --project datawatch-app --cluster testing \
                      --task "add loading spinner to the wear watchface"
```

## Project Profile fields

| field | type | notes |
|---|---|---|
| `name` | string | DNS label: lowercase, digits, hyphen. 1-63 chars |
| `description` | string | optional |
| `git.url` | string | required |
| `git.branch` | string | default = repo's default branch |
| `git.provider` | string | `github` / `gitlab` / `local` / `""` (auto) |
| `git.auto_pr` | bool | open a PR on session complete (Sprint 5+) |
| `image_pair.agent` | string | one of `agent-claude`, `agent-opencode`, `agent-gemini`, `agent-aider` |
| `image_pair.sidecar` | string | one of `lang-{go,node,python,rust,kotlin,ruby}`, `tools-ops`, or empty for solo |
| `env` | `map[string]string` | injected into both containers |
| `memory.mode` | string | `shared` / `sync-back` (default) / `ephemeral` |
| `memory.namespace` | string | default = `project-<name>` |
| `memory.shared_with` | `string[]` | opt-in cross-profile memory visibility |
| `idle_timeout` | duration | 0 = no timeout |
| `allow_spawn_children` | bool | permits recursion; paired budgets below |
| `spawn_budget_total` | int | cap on total children ever spawned |
| `spawn_budget_per_minute` | int | rate cap |
| `post_task_hooks` | `string[]` | shell commands run in the sidecar after task done |

## Cluster Profile fields

| field | type | notes |
|---|---|---|
| `name` | string | DNS label |
| `kind` | string | `docker` / `k8s` / `cf` (cf reserved for Sprint 8+) |
| `context` | string | kubectl context (k8s) — required unless endpoint set |
| `endpoint` | string | explicit API endpoint, usually for docker host URL |
| `namespace` | string | k8s namespace, default `default` |
| `image_registry` | string | `host/project`, overrides .env.build |
| `image_pull_secret` | string | k8s secret name for private registry |
| `default_resources.{cpu,mem}_{request,limit}` | string | k8s-style, e.g. `100m`, `256Mi` |
| `trusted_cas` | `string[]` | PEM blobs projected into worker Pods — fixes private-CA registry pulls from inside workers |
| `creds_ref.provider` | string | `file` / `env` / `k8s-secret` / `vault` (latter two stub until Sprint 8) |
| `creds_ref.key` | string | path/name/id interpreted by the provider |
| `network_policy_ref` | string | pre-existing NetworkPolicy to bind to worker Pod |
| `parent_callback_url` | string | override; empty = auto-detect |

## Channels

### REST

```
GET    /api/profiles/projects
POST   /api/profiles/projects           # body = full profile JSON
GET    /api/profiles/projects/{name}
PUT    /api/profiles/projects/{name}    # body = full profile JSON; URL wins on name
DELETE /api/profiles/projects/{name}
POST   /api/profiles/projects/{name}/smoke

(same six routes under /api/profiles/clusters/…)
```

Status codes: 201 create, 200 get/put, 204 delete, 404 unknown name,
409 duplicate, 400 invalid body, 422 smoke validation failed (profile
exists but not valid), 503 when the store isn't wired.

### MCP

Six tools, `kind: "project" | "cluster"`:

```
profile_list    kind
profile_get     kind, name
profile_create  kind, body          # JSON string
profile_update  kind, name, body
profile_delete  kind, name
profile_smoke   kind, name
```

All proxy to the REST API on 127.0.0.1. `profile_smoke` forwards 422
(validation failure) as a successful tool result so the LLM can see
the errors and fix them.

### CLI

```
datawatch profile project list [-f table|json|yaml]
datawatch profile project show <name> [-f json|yaml]
datawatch profile project create [-f file]   # JSON or YAML from file or stdin
datawatch profile project update <name> [-f file]
datawatch profile project delete <name>
datawatch profile project smoke <name>   # exit 2 on validation failure

datawatch profile cluster …              # identical subcommand shape
```

### Comm channels (signal, telegram, discord, slack, matrix, webhook)

Read-only over chat. Create/update/delete intentionally *not* exposed
over chat — the blast radius of a mis-typed profile change from a
chat line is too large.

```
profile project list
profile cluster list
profile project show <name>
profile cluster show <name>
profile project smoke <name>
profile cluster smoke <name>
```

### Web UI

Settings → General → two cards:

- **Project Profiles** — list, + Add, per-row [Smoke] [Edit] [×]
- **Cluster Profiles** — same shape

Each editor has a **Form view** (typed inputs, agent/sidecar dropdowns)
and a **YAML view** toggle for direct-paste usage.

## Smoke test

`smoke` is a cheap client-side validation pass. It returns:

```json
{
  "name": "datawatch-app",
  "checks": [
    "profile loaded",
    "validation",
    "smoke complete"
  ],
  "warnings": [],
  "errors": [],
  "ran_at": "2026-04-18T18:11:18Z"
}
```

A profile is "passing" iff `errors` is empty. Warnings surface things
the operator should know about but that don't block spawn (e.g.
`Kind=cf: Cloud Foundry driver not implemented`).

Sprint 4's K8s driver will add a deeper "probe" variant that actually
connects to the cluster and tries a dry-run pod create.

## Encryption at rest

When the daemon is started with `--secure`, the profile stores are
AES-256-GCM encrypted under the session key. Same pattern as the
schedule, alert, filter, and memory stores.

Switching `--secure` on requires re-creating every profile once; the
old plaintext files won't decrypt.

## Validation rules (server-side, enforced on create + update)

- Name must match `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`
- Project: `git.url` required, `image_pair.agent` must match a known
  agent-* image, `image_pair.sidecar` if set must match a known
  lang-*/tools-* image, `memory.mode` must be one of the three
- Project: spawn budgets require `allow_spawn_children: true`
- Cluster: k8s kind requires `context` or `endpoint`
- Cluster: each `trusted_cas[]` must contain `BEGIN CERTIFICATE`
- Cluster: if `creds_ref.provider` set, `creds_ref.key` required

Every channel reaches the same validator: you'll see the same error
whether you POST via REST, call `profile_create` via MCP, or save the
form in the Web UI.

## Known gaps (intentional)

- `fallback_chain` field (BL21 — alternate profiles to try on failure)
  is NOT in this release. Add in a later sprint if needed.
- `datawatch config set` CLI command doesn't exist for arbitrary config
  keys; profile CRUD is done through `datawatch profile …`.
- Create/update over comm channels is disabled by design; use the UI,
  CLI, API, or MCP for those.
