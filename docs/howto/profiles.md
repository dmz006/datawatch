# How-to: Project + Cluster Profiles

Project Profiles describe **what** to do (repo, agent + sidecar
images, memory policy, spawn budgets, env). Cluster Profiles describe
**where** to run (Docker / Kubernetes, namespace, registry, creds).
The two are orthogonal — one Project Profile can spawn on many
clusters, one Cluster Profile hosts many projects.

For full field-level reference (every key, every default), see
[`docs/profiles.md`](../profiles.md). This page is the operator
walkthrough.

## Base requirements

- Daemon up. Profiles are stored at `~/.datawatch/profiles/*.json`
  (or transparently encrypted when the daemon is started with
  `--secure`).
- For container worker spawning: `agents.enabled: true` and a Docker
  socket reachable from the daemon (or a kubeconfig pointed at your
  cluster).
- For PRD-driven daemon-side clones (no agents involved): just the
  profile — the daemon clones into `<data_dir>/workspaces/` itself.

## Where profiles surface

Per the project's "every feature on every interface" rule, profile
CRUD is reachable through every channel:

| Surface | Path |
|------|------|
| YAML | `~/.datawatch/profiles/project_<name>.json` / `cluster_<name>.json` |
| Web UI | Settings → General → Project Profiles / Cluster Profiles |
| REST | `GET / POST / PUT / DELETE /api/profiles/projects` and `/api/profiles/clusters` |
| MCP | `profile_project_*` and `profile_cluster_*` tools |
| CLI | `datawatch profile project ...` / `datawatch profile cluster ...` |
| Comm | `profile project add`, `profile cluster list`, etc. — same shape from Signal/Telegram/Slack |

## Walkthrough — minimum project + cluster profile

The smallest pair that lets you spawn a working ephemeral worker.

### 1. Project profile

```bash
datawatch profile project add --name datawatch-app \
  --repo https://github.com/dmz006/datawatch-app \
  --branch main \
  --agent agent-claude \
  --sidecar lang-kotlin
```

Or via the Web UI: Settings → General → Project Profiles → **+ Add**.
Fill `name`, `git.url`, `git.branch`, `image_pair.agent`,
`image_pair.sidecar`, hit Save.

REST equivalent:

```bash
curl -k -X POST https://localhost:8443/api/profiles/projects \
  -H "Content-Type: application/json" -d @- <<'EOF'
{
  "name": "datawatch-app",
  "git": { "url": "https://github.com/dmz006/datawatch-app", "branch": "main", "provider": "github" },
  "image_pair": { "agent": "agent-claude", "sidecar": "lang-kotlin" },
  "memory": { "mode": "sync-back" }
}
EOF
```

### 2. Cluster profile

```bash
datawatch profile cluster add --name local-docker \
  --driver docker \
  --image ghcr.io/dmz006/datawatch-agent-claude:latest \
  --memory 4Gi --cpu 2
```

Or REST:

```bash
curl -k -X POST https://localhost:8443/api/profiles/clusters \
  -H "Content-Type: application/json" -d @- <<'EOF'
{
  "name": "local-docker",
  "kind": "docker",
  "image": "ghcr.io/dmz006/datawatch-agent-claude:latest",
  "resources": { "memory": "4Gi", "cpu": "2" }
}
EOF
```

### 3. Use the pair

PWA New PRD modal (v5.26.30+): pick **datawatch-app** in the unified
Profile dropdown — the Cluster row appears, pick **local-docker**,
hit Create.

CLI agent spawn:

```bash
datawatch agent spawn --project datawatch-app --cluster local-docker \
  --task "fix the broken test in src/main.kt"
# → {"id":"agt_a1b2","state":"starting"}
```

REST PRD with both profiles:

```bash
curl -k -X POST https://localhost:8443/api/autonomous/prds \
  -H "Content-Type: application/json" -d '{
    "spec": "Add MQTT support to the device discovery pipeline",
    "project_profile": "datawatch-app",
    "cluster_profile": "local-docker"
  }'
```

## Common patterns

### Same project, multiple clusters

One Project Profile (`datawatch-app`) used against several Cluster
Profiles (`dev-laptop` / `staging-k8s` / `prod-k8s`). The Project
Profile is unchanged — only the cluster argument differs at spawn
time.

### k8s cluster profile

```yaml
name: testing
kind: k8s
context: testing             # kubectl context name
namespace: datawatch-workers
registry: ghcr.io/dmz006     # where to pull agent images from
trusted_cas: []              # optional extra CA PEMs to trust
creds_ref: regcred           # k8s Secret holding image-pull creds
```

REST shape is the same — `kind: "k8s"` and the k8s-specific keys
populate.

### Per-project secrets (not in YAML)

Profile YAML never holds secrets. Use the Cluster Profile's
`creds_ref` to point at a k8s Secret (or Docker credential helper)
holding the registry login. For repo auth, leave the project
profile's `git.url` as HTTPS and let the BL113 token broker mint a
short-lived per-spawn token (v5.26.24+; see
[`docs/howto/container-workers.md`](container-workers.md)).

## Smoke-test your profile

```bash
# Daemon-side clone path (no agent spawn needed)
curl -k -X POST https://localhost:8443/api/sessions/start \
  -H "Content-Type: application/json" -d '{
    "task": "echo hello from a clone",
    "project_profile": "datawatch-app",
    "backend": "claude-code"
  }'
# Watch the daemon log for: [session] reaped ephemeral workspace …
# (when you delete the session, v5.26.26+ auto-removes the clone tree)
```

## Troubleshooting

- **`project profile X has no git.url to clone from`** — the profile
  was saved with an empty `git.url`. Edit the profile in the PWA or
  re-POST with the URL set.
- **`unknown cluster profile X`** — case-sensitive name mismatch.
  `GET /api/profiles/clusters` to list, then re-spawn with the exact
  name.
- **Workspace clone leaks across crashes** — v5.26.27+ runs an
  orphan-workspace reaper at daemon startup. If you're on an older
  version, manually `rm -rf <data_dir>/workspaces/*` and upgrade.

## See also

- [`docs/profiles.md`](../profiles.md) — full field-level reference.
- [`docs/howto/container-workers.md`](container-workers.md) — uses
  profiles to spawn ephemeral workers.
- [`docs/howto/autonomous-planning.md`](autonomous-planning.md) —
  uses profiles in the PRD spawn path.
