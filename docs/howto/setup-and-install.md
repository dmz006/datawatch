# How-to: First-time setup and install

Get a working datawatch daemon on a fresh machine in ten minutes.
This how-to assumes you've never run datawatch before; pick **one**
install path, complete the smoke test, then jump to
[chat-and-llm-quickstart](chat-and-llm-quickstart.md) to wire your
first session.

## Pick an install path

| Path | When to pick it | Time to ready |
|------|-----------------|---------------|
| **One-liner** (recommended) | You have a Linux / macOS box with curl + bash | ~30 s |
| **Pre-built binary** | You're on Windows, an air-gapped host, or want to pin a version | ~1 min |
| **`go install`** | You already have Go ≥ 1.22 and want the latest tip | ~1 min |
| **Container** (`ghcr.io/dmz006/datawatch-parent-full`) | You want everything in one image (daemon + worker shells) | ~2 min |
| **Helm / Kubernetes** | You're running on a cluster, want secrets in `Secret`s, and need ephemeral worker Pods | ~5 min |

### Option A — one-liner

```bash
curl -fsSL https://raw.githubusercontent.com/dmz006/datawatch/main/install.sh | bash
```

The script downloads the right binary for your platform into
`~/.local/bin/datawatch`, sets the executable bit, and prints the
next-step `datawatch setup` invocation.

### Option B — pre-built binary

Grab the asset that matches your platform from the latest release and
drop it on `$PATH`:

```bash
# linux-amd64 example
curl -L -o ~/.local/bin/datawatch \
  https://github.com/dmz006/datawatch/releases/latest/download/datawatch-linux-amd64
chmod +x ~/.local/bin/datawatch
```

Available assets: `datawatch-{linux-amd64,linux-arm64,darwin-amd64,darwin-arm64,windows-amd64.exe}` plus a `checksums-vX.Y.Z.txt` you can verify with `sha256sum -c`.

### Option C — `go install`

```bash
go install github.com/dmz006/datawatch/cmd/datawatch@latest
```

Lands in `$(go env GOPATH)/bin/datawatch`. Use this only if you're
comfortable building from source; release notes don't apply to tip.

### Option D — container

```bash
docker pull ghcr.io/dmz006/datawatch-parent-full:latest
docker run -it --rm \
  -v ~/.datawatch:/root/.datawatch \
  -p 8443:8443 -p 8080:8080 \
  ghcr.io/dmz006/datawatch-parent-full:latest start
```

Persist the data dir to a volume so config + sessions survive
restarts.

### Option E — Helm on Kubernetes

The chart at `charts/datawatch/` ships the parent daemon as a
Deployment with a ConfigMap-rendered `config.yaml`, an optional PVC
for `~/.datawatch`, in-namespace RBAC for spawning ephemeral worker
Pods, and a Service. Every secret (API token, TLS cert/key, Postgres
URL, git token, kubeconfig for cross-cluster spawns) supports a
*dual-supply* pattern: inline for dev, or `existingSecret:` pointing
at a Secret you create out-of-band — pair the latter with SealedSecret
/ ExternalSecret / Vault for prod.

Minimum-viable install on a cluster you control:

```bash
# 1. Pre-create the API-token Secret. Operator picks the value;
#    chart references it by name so it never lands in values.yaml.
kubectl create namespace datawatch
kubectl -n datawatch create secret generic datawatch-api-token \
  --from-literal=DATAWATCH_API_TOKEN="$(openssl rand -hex 32)"

# 2. (optional) TLS cert. Skip for plain-HTTP dev clusters.
kubectl -n datawatch create secret tls datawatch-tls \
  --cert=tls.crt --key=tls.key

# 3. (optional) git token. Used for two purposes:
#    (a) BL113 worker token-broker (mints per-spawn tokens for ephemeral agents)
#    (b) v5.26.22 — daemon-side clone of project_profile-based PRDs
#        (POST /api/sessions/start with project_profile auto-rewrites
#        HTTPS git URLs to embed this token before cloning).
#    Use a fine-grained PAT with `repo:read` (read-only enough for
#    autonomous workers; bump to `repo` if PRDs need to push).
kubectl -n datawatch create secret generic datawatch-git-token \
  --from-literal=DATAWATCH_GIT_TOKEN="ghp_…"

# 4. Install. Override image.registry to your own.
helm install dw ./charts/datawatch \
  --namespace datawatch \
  --set image.registry=ghcr.io/dmz006 \
  --set image.tag=v5.26.0 \
  --set apiTokenExistingSecret=datawatch-api-token \
  --set tls.enabled=true \
  --set tls.existingSecret=datawatch-tls \
  --set gitToken.existingSecret=datawatch-git-token \
  --set persistence.enabled=true \
  --set persistence.size=20Gi
```

**NFS-backed persistence** — for shared / multi-Pod / off-node storage,
point `persistence.storageClass` at an NFS-fronted StorageClass. The
two most common providers:

```bash
# Provider A — CSI driver (recommended, dynamic provisioning):
#   https://github.com/kubernetes-csi/csi-driver-nfs
helm repo add csi-driver-nfs \
  https://raw.githubusercontent.com/kubernetes-csi/csi-driver-nfs/master/charts
helm install csi-driver-nfs csi-driver-nfs/csi-driver-nfs \
  --namespace kube-system

kubectl apply -f - <<'EOF'
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: nfs-datawatch
provisioner: nfs.csi.k8s.io
parameters:
  server: nfs.internal.example.com
  share: /exports/datawatch
mountOptions:
  - nfsvers=4.1
  - hard
reclaimPolicy: Retain
volumeBindingMode: Immediate
EOF

# Provider B — nfs-subdir-external-provisioner (lighter, sidecar-style):
#   https://github.com/kubernetes-sigs/nfs-subdir-external-provisioner
helm install nfs-subdir nfs-subdir-external-provisioner/nfs-subdir-external-provisioner \
  --namespace kube-system \
  --set nfs.server=nfs.internal.example.com \
  --set nfs.path=/exports/datawatch \
  --set storageClass.name=nfs-datawatch \
  --set storageClass.reclaimPolicy=Retain
```

Then point datawatch at it on install or upgrade:

```bash
helm upgrade dw ./charts/datawatch \
  --namespace datawatch \
  --reuse-values \
  --set persistence.storageClass=nfs-datawatch \
  --set persistence.accessMode=ReadWriteMany    # only if you'll scale > 1 replica
```

`accessMode=ReadWriteMany` matters only if you're running an HA pair
of parents (still pinned to `replicas: 1` in the chart for v1, but the
PVC binds correctly when you flip it later). For single-replica dev,
the default `ReadWriteOnce` is fine even on NFS — the export just
serves one Pod.

Verify the parent is running and reachable:

```bash
kubectl -n datawatch rollout status deploy/dw-datawatch
kubectl -n datawatch port-forward svc/dw-datawatch 8443:8443
# https://localhost:8443 — accept the self-signed cert, paste the API
# token from datawatch-api-token when the PWA prompts.
```

For cross-cluster worker spawns, project a kubeconfig Secret that
knows about every cluster the parent should reach:

```bash
kubectl -n datawatch create secret generic datawatch-kubeconfig \
  --from-file=config=$HOME/.kube/multi-cluster-config

helm upgrade dw ./charts/datawatch \
  --namespace datawatch \
  --reuse-values \
  --set kubeconfig.existingSecret=datawatch-kubeconfig
```

For per-cluster observer DaemonSets (Shape C — node-level CPU /
memory / network rolled up into the parent's federation card), set
`observer.shapeC.enabled=true` after registering the cluster as a
peer (`POST /api/observer/peers`) and seeding
`datawatch-observer-cluster-token`. Full chart reference:
`charts/datawatch/README.md`.

### Git credentials in k8s — picking a pattern

The v5.26.22 daemon-side clone for `project_profile`-based PRDs
needs git auth at clone time. The Helm chart supports three
patterns; pick whichever fits your repo provider + secret-management
story.

| Pattern | Setup | When to pick it |
|---------|-------|-----------------|
| **HTTPS + PAT in Secret** | `gitToken.existingSecret=datawatch-git-token` (above) — chart projects `DATAWATCH_GIT_TOKEN` into the daemon Pod's env. Daemon auto-rewrites `https://...` URLs to `https://x-access-token:<token>@...` at clone time. | GitHub / GitLab / cloud providers with PAT-based auth. Simplest. Token is auto-redacted from error output. |
| **SSH key in Secret** | `kubectl -n datawatch create secret generic datawatch-ssh \` `--from-file=id_ed25519=$HOME/.ssh/id_ed25519 --from-file=known_hosts=...`. Add to chart values: `ssh.existingSecret=datawatch-ssh`. Chart mounts at `/root/.ssh/`. | SSH URLs (`git@...`) or providers without PAT support. Works with deploy keys for repo isolation. |
| **F10 BL113 token broker** *(future)* | Daemon mints short-lived per-spawn tokens via the parent's TokenBroker. No long-lived secret in the Pod. | Multi-tenant deployments where each spawn should authorize independently. Lands in v5.26.23+. |

For HTTPS auth, the daemon redacts the embedded token from any error
output (`x-access-token:***@host`), so accidental log leaks of the
clone error don't expose the token. Pre-existing token-in-URL profiles
(`https://alice:secret@gitea.example.com/...`) are similarly redacted.

For SSH, no rewriting happens — the daemon shells out to `git clone`
with whatever the standard SSH agent / `~/.ssh` config does. Mount
both `id_ed25519` AND `known_hosts` so host-key verification doesn't
prompt. The chart's `ssh.existingSecret` template handles the mount;
values reference: `charts/datawatch/values.yaml`.

### Promote to systemd (Linux, single-host)

Once `datawatch start --foreground` is verified, drop the binary
under a systemd unit so it survives reboots.

```bash
sudo tee /etc/systemd/system/datawatch@.service > /dev/null <<'UNIT'
[Unit]
Description=datawatch control plane (%i)
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
User=%i
ExecStart=/usr/local/bin/datawatch start --foreground
Restart=on-failure
RestartSec=5
Environment=DATAWATCH_CONFIG=%h/.datawatch/config.yaml

[Install]
WantedBy=multi-user.target
UNIT

sudo systemctl daemon-reload
sudo systemctl enable --now datawatch@<your-user>
```

The `%i` template lets a single unit serve multiple operators on a
shared host (`datawatch@alice`, `datawatch@bob`).

### Cluster paths datawatch holds (Helm install only)

| Path | Source | Purpose |
|------|--------|---------|
| `/var/lib/datawatch/config.yaml` | ConfigMap → `subPath` mount | Render of `.Values.config` |
| `/var/lib/datawatch/` | PVC (when `persistence.enabled: true`) | sessions.json, profiles, memory.db, alerts |
| `/etc/datawatch/tls/` | tls Secret (when `tls.enabled: true`) | Server cert + key |
| `/etc/datawatch/kubeconfig/` | `kubeconfig.existingSecret` | Cross-cluster kubectl context |
| env `DATAWATCH_SERVER_TOKEN` | `apiTokenExistingSecret` | API bearer |
| env `DATAWATCH_POSTGRES_URL` | `postgres.existingSecret` | Episodic memory backend |
| env `DATAWATCH_GIT_TOKEN` | `gitToken.existingSecret` | Token broker provider |

Nothing reads from `/home/<operator>/...` at runtime in the cluster
path — the chart never mounts it and the deployment manifest never
references it.

### Self-managing config (Helm install only)

Once the parent is up, an operator (or — with `mcp.allow_self_config: true` — an in-process AI session per BL110) can tune everything else through the running daemon, no Pod restart needed for most knobs:

```bash
# From any host with kubectl + port-forward:
datawatch config set agents.image_tag v5.27.2
datawatch config set agents.idle_reaper_interval_seconds 30
datawatch profile create project ...    # via the REST API behind the scenes
```

The bootstrap-protected gate `mcp.allow_self_config` itself only
flips via direct YAML edit + `helm upgrade` — preserves the rule
that an AI can't grant itself the very permission that lets it
mutate config.

For RBAC, restrict the parent's Pod-create scope to its own namespace
in `values.yaml`:

```yaml
rbac:
  create: true   # generates a Role + RoleBinding for in-namespace spawns
# For cross-namespace spawns, set rbac.create: false and bind a
# ClusterRole out-of-band so the chart doesn't grant cluster-wide.
```

## Messaging backend setup (Signal / Telegram / Discord / Matrix)

The install paths above bring up the daemon with no messaging
backend wired. Add one when you want chat-channel control:

```bash
datawatch setup signal     # signal-cli pairing flow + group_id wizard
datawatch setup telegram   # bot token + chat_id wizard
datawatch setup discord    # webhook URL + channel mapping
datawatch setup matrix     # homeserver + access token + room id
```

Each wizard writes to `~/.datawatch/config.yaml` under the matching
section (`signal:`, `telegram:`, `discord:`, `matrix:`) and
hot-reloads via `datawatch reload` — no daemon restart required.
Per-backend deep dives live in
[`docs/howto/comm-channels.md`](comm-channels.md).

## Initial config wizard

```bash
datawatch setup
```

Walks you through:

1. **Bind addresses** — defaults to `0.0.0.0:8443` (HTTPS) +
   `0.0.0.0:8080` (HTTP redirect to TLS). Change with `--host`/`--port`
   on `datawatch start` if needed.
2. **TLS certificate** — auto-generates a self-signed cert under
   `~/.datawatch/tls/` on first run.
3. **First-time auth token** — generated and printed once; saved to
   `~/.datawatch/token`. Header for API calls:
   `Authorization: Bearer <token>`.
4. **Default LLM backend** — claude / ollama / etc. You can skip this
   and configure later in [chat-and-llm-quickstart](chat-and-llm-quickstart.md).

Re-run any time to re-walk individual sections (`datawatch setup ebpf`,
`datawatch setup signal`, etc.).

## Start the daemon

```bash
datawatch start            # daemonize in the background
datawatch start --foreground   # run in the current terminal
```

Verify it's up:

```bash
datawatch ping             # → "pong"
datawatch status           # daemon + sessions one-liner
```

Open the PWA in a browser:

```
https://localhost:8443
```

You'll get the self-signed-cert warning the first time — accept it,
then the PWA loads at the Sessions tab.

The Settings → Monitor tab confirms the daemon is healthy and shows
which subsystems are wired (CPU/mem/disk/GPU, daemon RSS, RTK token
savings, episodic memory, ollama runtime tap if configured):

![Settings → Monitor — System Statistics card](screenshots/settings-monitor.png)

Settings → About surfaces the version, the GitHub link for the mobile
companion, and the orphan-tmux maintenance affordance:

![Settings → About tab](screenshots/settings-about.png)

## Smoke test — start your first session

```bash
datawatch session start --task "echo hello from datawatch"
```

The session appears in the PWA Sessions list within ~2 s with state
`running`, then `done`. Click into it to see the tmux output and the
captured response.

## Ready to code — clone a repo + run a real coding task

The smoke-test session above just runs a shell echo. To get to
actually-coding-against-an-LLM-in-30-seconds, you need (1) a project
checkout and (2) at least one LLM backend wired.

### 1. Clone the repo you want to work on

```bash
mkdir -p ~/code && cd ~/code
git clone git@github.com:your-org/your-repo.git
cd your-repo
```

If the parent runs in Kubernetes, the worker Pod needs to do the
clone instead — supply a git token via the `gitToken.existingSecret`
chart value (above) and the spawned worker resolves
`https://github.com/...` using the broker-minted credential. SSH
clones inside Pods need an additional Secret mounted at
`/root/.ssh/id_ed25519` plus a `known_hosts` — see
[`docs/howto/container-workers.md`](container-workers.md).

### 2. Wire one LLM backend

Pick whichever you have credentials for:

```bash
# Anthropic / claude-code (CLI must be installed + logged in once)
claude login

# Ollama (local, no key)
ollama serve &
ollama pull qwen2.5-coder:7b

# OpenWebUI (separate server)
datawatch setup llm   # walks api-key + endpoint
```

Re-run `datawatch setup llm` to flip which one is the default for new
sessions, or set per-session via `--llm-backend`.

### 3. First real coding session

```bash
datawatch session start \
  --project-dir ~/code/your-repo \
  --llm-backend claude-code \
  --task "read the README and summarize the project's purpose in 3 bullets, then list the top 3 areas where you'd start improving the test coverage"
```

What happens:

1. datawatch creates a tmux session with the working directory set to
   `--project-dir`, opens the LLM CLI inside it, and types the task.
2. The PWA Sessions tab shows `running`. Click in — tmux output on
   the **Output** tab, MCP channel chatter on the **Channel** tab.
3. When the LLM finishes, the session goes `waiting_input`. The
   **Response** button (📄 between Saved-Commands and arrows on the
   tmux toolbar) shows the latest captured response. Type a follow-up
   into the input bar, hit **Send**, and the loop continues.
4. Hit **Stop** in the PWA when you're done. Session output lives in
   `~/.datawatch/sessions/<full-id>/` after the tmux pane is gone.

### 4. (optional) Pre-commit / pre-push integration

Pipelines + DAGs let you gate task completion on lint + tests passing.
The fast path is a one-liner before-after pair:

```bash
datawatch session start \
  --project-dir ~/code/your-repo \
  --task "add a unit test for the cookie-jar serialization" \
  --before-cmd "make test" \
  --after-cmd  "make test && make lint"
```

`before-cmd` runs once before the session starts (sets a baseline);
`after-cmd` runs after the session reports done — non-zero exit
flips the session to `failed` and the verifier (if enabled) feeds the
failure back as a retry hint. Full pipeline + DAG semantics in
[`pipeline-chaining`](pipeline-chaining.md).

## What got installed where

| Path | Purpose |
|------|---------|
| `~/.local/bin/datawatch` | The binary itself |
| `~/.datawatch/config.yaml` | Operator-edited config (sane defaults, override anything) |
| `~/.datawatch/daemon.log` | Live daemon log (`tail -f` it) |
| `~/.datawatch/sessions/` | Per-session output + metadata |
| `~/.datawatch/autonomous/` | PRD store (BL24 / BL191) |
| `~/.datawatch/memory/` | Episodic memory + KG (when enabled) |
| `~/.datawatch/tls/` | Self-signed cert + key |

## Upgrades

```bash
datawatch update --check    # dry-run — what version would land
datawatch update            # download + install the latest minor
datawatch restart           # apply the new binary; preserves tmux sessions
```

The PWA also exposes a one-click "Update" + "Restart" pair under
Settings → About.

## Reachability across channels

| Channel | Action | Command |
|---------|--------|---------|
| CLI | first-time setup | `datawatch setup` |
| CLI | start / restart | `datawatch start` / `datawatch restart` |
| CLI | health probe | `datawatch ping` / `datawatch status` |
| REST | health | `GET /healthz` |
| MCP | (no setup tools — daemon must already be running) | — |
| PWA | post-install | Settings → About → Update / Restart |

## See also

- [How-to: Chat + LLM quickstart](chat-and-llm-quickstart.md) — wire your first conversational backend
- [How-to: Daemon operations](daemon-operations.md) — day-two ops (start/stop/upgrade/diagnose)
- [`docs/setup.md`](../setup.md) — exhaustive install reference
- [`docs/operations.md`](../operations.md) — systemd unit / hardened deploy
