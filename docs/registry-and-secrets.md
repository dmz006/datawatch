# Registry, Kubernetes, and secret configuration

Datawatch is built so every registry, cluster, and credential is
**operator-configured per-deployment** — there are no hard-coded
production hosts in the binary, the Helm chart, or the Dockerfiles.
This page is the one place to learn how to point datawatch at *your*
container registry, *your* Kubernetes cluster, *your* GitHub /
GitLab account, and *your* secret material.

> **Rule.** Per [AGENT.md](../AGENT.md) — every configurable item is
> reachable through YAML, REST API, MCP, comm channels, CLI, and (for
> read paths) the web UI. Anything below that names a config field is
> available through every channel; the YAML key is the canonical form.

---

## 1. Container registry

Datawatch ships seven worker images plus a parent control-plane
image. None of them are hosted on a public registry yet — operators
build + push to their own.

### Build-time (Dockerfiles + Makefile)

The Dockerfiles take a `REGISTRY` build-arg with a deliberate
local-dev default of `localhost:5000/datawatch` so a fresh clone +
`make container-base` works without configuration. Override at build
time three ways:

```sh
# 1. command-line (one-off)
make container-base REGISTRY=ghcr.io/your-org/datawatch PUSH=true

# 2. .env.build  (recommended for repeated builds)
cp .env.build.example .env.build
$EDITOR .env.build         # set REGISTRY=ghcr.io/your-org/datawatch
make container-all PUSH=true

# 3. shell environment (CI)
export REGISTRY=ghcr.io/your-org/datawatch
export PUSH=true
make container-all
```

`.env.build` is gitignored — copy from `.env.build.example` (which
has placeholder examples for ghcr / harbor / gitlab / ECR / local-dev)
and edit. Verify your overrides never land in git:

```sh
git check-ignore -v .env.build         # should print: .gitignore:N:.env.build
git ls-files | grep -E '^\.env\.build$' # should print nothing
```

### Run-time (datawatch daemon)

Once built + pushed, point the daemon at your registry by setting
`agents.image_prefix` (configurable via every channel):

```yaml
# config.yaml
agents:
  image_prefix: ghcr.io/your-org/datawatch    # used when ClusterProfile.image_registry is empty
  image_tag: v2.4.5                            # default; overridable per spawn
```

```sh
# REST
curl -X PUT https://parent/api/config \
  -d '{"agents.image_prefix":"ghcr.io/your-org/datawatch"}'

# MCP
config_set key=agents.image_prefix value=ghcr.io/your-org/datawatch

# comm channel (Signal/Telegram/etc.)
configure agents.image_prefix=ghcr.io/your-org/datawatch
```

For per-cluster overrides — useful when prod uses Harbor and dev uses
a local registry — set `image_registry` on the Cluster Profile:

```yaml
# datawatch profile cluster create prod
kind: k8s
context: prod
image_registry: harbor.example.com/datawatch
image_pull_secret: harbor-creds   # k8s Secret of type kubernetes.io/dockerconfigjson
```

### Helm chart

The chart's `image.registry` value has **no default** to force the
operator to set it explicitly:

```sh
helm install dw ./charts/datawatch -n datawatch --create-namespace \
  --set image.registry=ghcr.io/your-org/datawatch \
  --set image.tag=v2.4.5
```

When the registry is private, also set `imagePullSecret`:

```sh
kubectl -n datawatch create secret docker-registry ghcr-creds \
  --docker-server=ghcr.io \
  --docker-username=your-user \
  --docker-password="$GHCR_TOKEN"

helm install dw ./charts/datawatch -n datawatch --create-namespace \
  --set image.registry=ghcr.io/your-org/datawatch \
  --set image.tag=v2.4.5 \
  --set imagePullSecret=ghcr-creds
```

---

## 2. Kubernetes context + namespace

The K8s driver shells out to `kubectl` and reads contexts from the
operator's `~/.kube/config` (or in-cluster ServiceAccount token when
the daemon itself runs as a Pod under the Helm chart). Datawatch
never embeds kubeconfigs.

### Per-cluster (Cluster Profile)

```yaml
# datawatch profile cluster create staging
kind: k8s
context: staging-eks         # the kubectl context name from your kubeconfig
namespace: datawatch-workers # where worker Pods land
image_registry: ghcr.io/your-org/datawatch
image_pull_secret: ghcr-creds
trusted_cas:                 # PEM blobs the worker Pod should trust
  - |
    -----BEGIN CERTIFICATE-----
    ...
    -----END CERTIFICATE-----
parent_callback_url: https://datawatch.staging.example.com   # what worker Pods dial home to
```

`context: ""` + the parent running in-cluster = use the projected
ServiceAccount token. The Helm chart provisions a namespace-scoped
Role + RoleBinding granting `pods: create/get/list/delete +
pods/log + pods/exec` and `configmaps + secrets`. Cross-namespace or
cross-cluster requires bringing your own ClusterRole + binding.

### kubectl binary path

```yaml
agents:
  kubectl_bin: kubectl       # default; set to "oc" for OpenShift or a vendored path
```

---

## 3. Git provider tokens (GitHub / GitLab)

Datawatch's S5 token broker mints short-lived per-spawn git tokens
by shelling out to the operator's `gh` (or, when implemented, `glab`)
CLI — same arch decision as docker/kubectl. Operator sets up `gh
auth login` once on the parent host; datawatch uses the resulting
token + revokes it through the same CLI.

```sh
# one-time on the parent host (or in the Helm chart's parent Pod)
gh auth login          # follow the device-flow prompts
gh auth status         # confirms scope + account
```

The broker honours `agents.bootstrap_token_ttl_seconds` (default
300) for spawn-token TTL. Per-spawn git tokens are TTL-capped via
the broker's `MaxTTL` (default 1h) and revoked on session end +
periodic sweep — see [agents.md → Git provider + token broker](agents.md).

For GitLab, set `git.provider: gitlab` on the Project Profile;
schema is accepted but the implementation is currently a stub
returning `ErrNotImplemented` (BL164 / Sprint 5 prereq).

---

## 4. Secrets handling

### What datawatch reads

| Secret | Source | How datawatch reads it |
|--------|--------|------------------------|
| Server bearer token (API auth) | `server.token` in config.yaml or `DATAWATCH_SERVER_TOKEN` env | YAML or env, never on the wire |
| Encrypted-config password | interactive prompt or `DATAWATCH_SECURE_PASSWORD` env | only when `--secure` is on |
| GitHub PAT | `gh auth token` (read on demand) | shell-out, never persisted |
| Bootstrap token (spawn) | minted in-memory; injected into spawned container env | single-use, never logged |
| TLS cert + key | `server.tls_cert` + `server.tls_key` paths | PEM files on disk |
| Postgres URL | `memory.postgres_url` or `DATAWATCH_POSTGRES_URL` env | Helm chart Secret in K8s |

### Gitignored by default

```text
# .gitignore (relevant entries)
config.yaml          # the actual operator config (with secrets)
.env                 # arbitrary dotenv files
.env.build           # container build environment
*.log
*.db                 # local memory + sessions databases
.claude/             # local agent state
```

`config.yaml.example` (in the repo root, when present) shows the
shape with placeholder values — never commit a real `config.yaml`.

### Helm chart secret handling

`apiToken`, `postgres.url`, and inline `tls.cert/key` values land in
a Kubernetes `Secret`. **For prod, use `tls.existingSecret` and
provision the cert via SealedSecret / ExternalSecret / Vault sidecar
— do not pass cert/key inline through `--set`.**

```sh
# create a sealed/external secret out-of-band, then:
helm install dw ./charts/datawatch \
  --set tls.enabled=true \
  --set tls.existingSecret=datawatch-tls-prod
```

---

## 5. Auditing your own deployment

```sh
# 1. is .env.build still gitignored?
git check-ignore -v .env.build

# 2. is config.yaml still gitignored?
git check-ignore -v config.yaml

# 3. did any harbor.example.com / your-org / actual-host leak in?
git grep -i 'your-actual-registry\|your-internal-host'

# 4. did any kube-context name leak?
git grep -E 'context:\s+(prod|production|live)'

# 5. any `.kube/` or `kubeconfig` files in tree?
git ls-files | grep -iE 'kube.*config|\.kube/'
```

The `tests/integration/spawn_docker.sh` and `spawn_k8s.sh` smoke
scripts default to `busybox:latest` so a fresh clone can run the
REST flow without pulling any private image — set
`DATAWATCH_SMOKE_IMAGE=ghcr.io/your-org/datawatch:slim-vX` +
`RUN_BOOTSTRAP=1` to exercise the full bootstrap path.

---

## 6. Cluster-shared volumes (BL114)

Spawned workers are sealed by default — each container is its own
filesystem with no cross-session visibility. When multiple sessions
need to share artifacts (build caches, large datasets, prompt-
generated outputs that the next agent should consume) the operator
opts in per-cluster via `shared_volumes`.

### Schema

```yaml
# ~/.datawatch/profiles/cluster.<name>.yaml
shared_volumes:
  - name: dataset-cache
    mount_path: /workspace/cache
    read_only: false
    host_path: /var/lib/datawatch/cache    # docker
  - name: shared-research
    mount_path: /workspace/shared
    read_only: true                         # safer default for shared data
    nfs:                                    # k8s
      server: 198.51.100.10                 # use IANA TEST-NET in examples
      path:   /exports/datawatch-shared
  - name: build-output
    mount_path: /workspace/out
    pvc: datawatch-shared-output            # k8s PersistentVolumeClaim name
```

Exactly one of `host_path | nfs | pvc` must be set per entry. The
schema is validated at profile-create time.

### Driver behaviour

- **Docker driver:** translates each entry with a non-empty
  `host_path` into `-v <host_path>:<mount_path>[:ro]`. NFS and PVC
  sources are silently skipped (operator pre-mounts the NFS share
  on the host at any path they choose, then references that path
  via `host_path` in a separate `shared_volumes` entry — datawatch
  does *not* infer a `/mnt/...` prefix).
- **K8s driver:** renders `volumes` + `volumeMounts` blocks into the
  Pod manifest. NFS, PVC, and HostPath sources are all honoured.

### Safety

Default to `read_only: true` whenever the share holds data the
worker shouldn't mutate. The driver injects the mount as-is —
datawatch does not enforce read-only on the operator's behalf, so
the profile flag IS the enforcement.

When testing against a real NFS share, mount read-only first to
verify visibility — only re-spawn read-write once you've confirmed
the workers see (and only the right workers see) the share.

## 7. Why this approach

- **No upstream defaults that leak the maintainer's environment.**
  Local-dev defaults (`localhost:5000`, `127.0.0.1`) are safe; prod
  hosts must be operator-configured.
- **Same surface across channels.** The above YAML keys are the
  same shape via REST, MCP, comm channels, and CLI — operators
  can switch between channels without learning new field names.
- **No re-deploy required to retarget.** Almost every value above
  is hot-reloadable via `PUT /api/config` (Helm-managed values
  require an `helm upgrade`; everything else is runtime). Cluster
  Profile changes apply on the next spawn; `agents.image_prefix`
  changes apply on the next spawn; restart only required for
  bind-port / TLS-cert changes.
- **Audit-by-default.** Every config write lands in the daemon log
  with operator + timestamp; secret rotations land in the token
  broker's `audit.jsonl`.
