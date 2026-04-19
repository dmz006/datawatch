# Install

datawatch ships two install paths. Pick the one that matches where the
control plane will live.

| Path | Use when | Lifecycle | Secrets |
|------|----------|-----------|---------|
| **Single host** | dev box, lab, single-tenant prod | systemd unit on the host | `~/.datawatch/config.yaml` (gitignored) + `gh auth login` for git creds |
| **Cluster (Helm)** | production, multi-cluster, "datawatch runs itself" | k8s Deployment + Service | k8s Secrets (or SealedSecret/ExternalSecret) referenced from `values.yaml`; **no operator home dir on the parent** |

The cluster path is the BL113 "self-managing platform" target: an
operator runs one `helm install` and datawatch boots into the cluster
with every credential resolved from cluster Secrets at runtime — the
parent never reads the operator's `~/.kube/config` or `gh` token cache.

---

## 1. Single-host install

Pre-flight:

```bash
# Linux (amd64 / arm64) or macOS (Apple silicon / Intel)
which docker          # for Docker driver spawns; optional for k8s-only setups
which kubectl         # for K8s driver spawns; optional for docker-only setups
which gh || which glab # for the git token broker
```

Install:

```bash
# 1. Download the prebuilt binary from the GitHub release for your
#    OS / arch and install to /usr/local/bin (or anywhere on PATH).
#    Example for linux/amd64:
gh release download --repo dmz006/datawatch \
  --pattern 'datawatch-linux-amd64' --output /usr/local/bin/datawatch
chmod +x /usr/local/bin/datawatch

# 2. Initialise the config.
datawatch config init    # interactive wizard
# OR: datawatch config generate > ~/.datawatch/config.yaml  (annotated default)

# 3. (Optional) authenticate the git provider so the token broker can mint per-spawn tokens.
gh auth login            # GitHub
glab auth login          # GitLab

# 4. Start as a foreground process to verify, then promote to systemd.
datawatch start --foreground
```

Promote to systemd (Linux):

```bash
sudo tee /etc/systemd/system/datawatch.service > /dev/null <<'UNIT'
[Unit]
Description=datawatch control plane
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
User=%i               # systemd %i is the operator's username
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

Validate:

```bash
curl -sS http://127.0.0.1:8080/healthz                    # liveness
datawatch status                                          # session list
datawatch config get agents.image_prefix                  # config readback
```

---

## 2. Cluster (Helm) install — self-managing path

End state: every secret datawatch needs at runtime lives in a k8s
Secret. The operator hands the chart references to those Secrets; the
chart projects them as env vars + mounted files. **No operator home
directory is referenced at runtime.**

### 2.1 Pre-flight

```bash
helm repo add datawatch https://your.helm.repo/  # or use the chart from this repo
kubectl create namespace datawatch
```

### 2.2 Operator-supplied Secrets (one-time)

```bash
# (a) API bearer token for /api/* — pick any random 32+ chars.
kubectl -n datawatch create secret generic datawatch-api-token \
  --from-literal=DATAWATCH_API_TOKEN="$(openssl rand -hex 32)"

# (b) Git token for the broker (needed when projects spawn workers
#     that clone private repos). Use a fine-grained PAT (GitHub) or
#     project access token (GitLab).
kubectl -n datawatch create secret generic datawatch-git-token \
  --from-literal=DATAWATCH_GIT_TOKEN="ghp_xxx_or_glpat_xxx"

# (c) (optional) Postgres connection for episodic memory.
kubectl -n datawatch create secret generic datawatch-postgres \
  --from-literal=DATAWATCH_POSTGRES_URL="postgres://dw:pass@pg.dwsys/datawatch?sslmode=require"

# (d) (optional) kubeconfig for cross-cluster spawns. Skip when the
#     parent only spawns into its own cluster (uses ServiceAccount).
kubectl -n datawatch create secret generic datawatch-kubeconfig \
  --from-file=config="$HOME/.kube/config"     # one-shot upload; rotate via SealedSecret in prod
```

For production, replace each `kubectl create secret` with a
SealedSecret / ExternalSecret / Vault CSI projection so the secret
material never sits in cluster etcd unsealed.

### 2.3 values overrides

```yaml
# my-values.yaml
image:
  registry: registry.example.com/datawatch
  tag: v3.0.0

publicURL: https://datawatch.example.com   # how spawned worker Pods dial home

apiTokenExistingSecret: datawatch-api-token

gitToken:
  existingSecret: datawatch-git-token

postgres:
  existingSecret: datawatch-postgres

# Skip this block to use the parent's in-cluster ServiceAccount
# (single-cluster spawns only).
kubeconfig:
  existingSecret: datawatch-kubeconfig

persistence:
  enabled: true
  size: 20Gi
  storageClass: longhorn       # or your cluster's default

# Restrict the parent's Pod-create RBAC to its own namespace; for
# cross-namespace spawns set rbac.create: false and provide a
# ClusterRole binding out-of-band.
rbac:
  create: true
```

### 2.4 Install + verify

```bash
helm install dw datawatch/datawatch \
  -n datawatch \
  -f my-values.yaml

# Wait for the Pod.
kubectl -n datawatch wait --for=condition=ready pod -l app.kubernetes.io/name=datawatch --timeout=120s

# Liveness + readyz.
kubectl -n datawatch port-forward svc/dw-datawatch 8080:8080 &
curl -sS http://127.0.0.1:8080/healthz

# Confirm the projected env wired correctly (no values, just keys present).
kubectl -n datawatch exec deploy/dw-datawatch -- env | grep -E '^(DATAWATCH_|KUBECONFIG)' | cut -d= -f1
```

### 2.5 Self-managing config

Once the parent is up, an operator (or, with `mcp.allow_self_config: true`,
an in-process AI session per BL110) can tune everything else through
the running daemon — no Pod restart needed for most knobs:

```bash
# From any host with kubectl + port-forward:
datawatch config set agents.image_tag v3.0.1
datawatch config set agents.idle_reaper_interval_seconds 30
datawatch profile create project ...    # via the REST API behind the scenes
```

The bootstrap-protected gate `mcp.allow_self_config` itself only
flips via direct YAML edit + `helm upgrade` — preserves the rule
that an AI can't grant itself the very permission that lets it
mutate config.

### 2.6 Cluster paths datawatch holds

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

---

## 3. Upgrades

```bash
# Single-host
datawatch update --check                # see if a newer release is available
datawatch update                        # downloads + replaces the binary

# Helm
helm upgrade dw datawatch/datawatch -n datawatch -f my-values.yaml
```

Both paths preserve the daemon state (`~/.datawatch/` on host;
the PVC in cluster).

---

## 4. Backlog adjacent items

- **BL113 (this doc)** — install paths
- **BL115** — pre-release functional test pass against a real K8s
  cluster + NFS share, results captured in `docs/testing.md`
- **BL110** — the `mcp.allow_self_config` gate the §2.5 self-managing
  flow depends on
- **BL111** — `secrets.Provider` interface so `~/.datawatch/secrets/*`
  + future Vault/CSI integrations all resolve through the same surface
- **BL114** — `cluster.shared_volumes` for cross-session NFS / PVC
  mounts (per `docs/registry-and-secrets.md` §6)
