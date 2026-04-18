# Container build & registry guide

How to build the F10 image taxonomy locally, push to a registry the
Kubernetes cluster can pull from, and configure cluster nodes to trust
private registries.

## Quick start

```bash
# 1. Configure the registry once
cp .env.build.example .env.build
$EDITOR .env.build              # set REGISTRY, e.g. harbor.dmzs.com/datawatch

# 2. Authenticate (if registry requires it)
docker login harbor.dmzs.com

# 3. Build everything (multi-arch, push to registry)
make container

# 4. Or single image, single arch, local docker only (fast dev loop)
make container-load             # agent-base only, no push
make container-agent-claude PUSH=false
```

## Image taxonomy

| Image | Purpose | Approx size |
|---|---|---|
| `agent-base` | Foundation — datawatch + tmux + git + gh + rtk | ~300MB |
| `agent-claude` | Anthropic Claude Code (236MB native ELF) | ~520MB |
| `agent-opencode` | OpenCode | ~600MB |
| `agent-gemini` | Google Gemini CLI | ~580MB |
| `agent-aider` | Aider + litellm | ~1.4GB |
| `lang-go` | Go 1.25 + gopls + golangci-lint + delve | ~890MB |
| `lang-node` | Node 22 + pnpm + ts + tsx + eslint + bun | ~950MB |
| `lang-python` | Python + uv + poetry + ruff + pyright | ~810MB |
| `lang-rust` | rustup + clippy + rust-analyzer | ~1.2GB |
| `lang-kotlin` | JDK 17 + Kotlin + Gradle + Android cmd-tools | ~960MB |
| `lang-ruby` | Ruby + bundler + rubocop + ruby-lsp | ~770MB |
| `tools-ops` | kubectl + terraform + yq + aws + gcloud + dig/nmap/mtr | ~1.2GB |
| `parent-full` | agent-base + signal-cli + JRE (control plane) | ~510MB |

Composition: pair one `agent-*` with one `lang-*` (or `tools-*`) in the
same Pod sharing `/workspace`. Examples in
[composition-examples.md](composition-examples.md).

## .env.build

Gitignored. Sets the build context; `.env.build.example` documents the
shape. Key variables:

| Variable | Default | Notes |
|---|---|---|
| `REGISTRY` | `localhost:5000/datawatch` | Push target. Format `<host>/<project>` |
| `PLATFORMS` | `linux/amd64,linux/arm64` | buildx platforms; multi-arch requires `PUSH=true` |
| `PUSH` | `false` | When `true`, pushes after build |
| `AGENT_TYPES` | `claude opencode gemini aider` | Subset of agents to build |
| `LANG_TYPES` | `go node python rust kotlin ruby` | Subset of languages |
| `TOOL_TYPES` | `ops` | Subset of tools images |
| `CONTAINER_TAG` | `v$(VERSION)` | Override for branch builds |

## Make targets

```
make container               # all variants (chain-ordered, dependency-aware)
make container-load          # agent-base, single-arch, local docker
make container-agent-claude  # one variant
make container-lang-kotlin   # …
make container-tools-ops     # …
make container-parent-full   #
make container-tarball       # xz tarballs in dist/ for air-gap
make container-clean         # nuke buildx cache
make container-upgrade       # bump pinned tool versions (dry-run)
make container-upgrade APPLY=1   # actually rewrite ARG defaults
make registry-up             # local registry:2 fallback
make registry-down
```

## Choosing a registry

### Harbor (preferred for this dev environment)

Harbor at `harbor.dmzs.com`. Self-signed Pivotal-issued root CA — both
the docker daemon AND containerd snapshotter need to trust it.

**Docker daemon** (for `docker push`):

```bash
# Fetch Harbor's root CA via its API
curl -sk https://harbor.dmzs.com/api/v2.0/systeminfo/getcert -o /tmp/harbor-ca.crt

# Install it where docker's daemon HTTP client looks
sudo cp /tmp/harbor-ca.crt /usr/local/share/ca-certificates/harbor-dmzs.crt
sudo update-ca-certificates
sudo systemctl restart docker

# Verify
docker login harbor.dmzs.com
```

**containerd** (for the docker daemon's containerd snapshotter mode,
docker 25+):

```bash
sudo mkdir -p /etc/containerd/certs.d/harbor.dmzs.com
sudo cp /tmp/harbor-ca.crt /etc/containerd/certs.d/harbor.dmzs.com/ca.crt

sudo tee /etc/containerd/certs.d/harbor.dmzs.com/hosts.toml > /dev/null <<EOF
server = "https://harbor.dmzs.com"
[host."https://harbor.dmzs.com"]
  capabilities = ["pull", "push", "resolve"]
  ca = "/etc/containerd/certs.d/harbor.dmzs.com/ca.crt"
EOF

# Tell containerd to look at /etc/containerd/certs.d
grep -q 'config_path' /etc/containerd/config.toml || \
  sudo tee -a /etc/containerd/config.toml > /dev/null <<'EOF'

[plugins."io.containerd.cri.images".registry]
  config_path = "/etc/containerd/certs.d"
EOF

sudo systemctl restart containerd && sudo systemctl restart docker
```

**Quicker workaround** (no CA install — disables TLS verify for harbor only):

```bash
echo '{"insecure-registries":["harbor.dmzs.com"]}' | sudo tee /etc/docker/daemon.json
sudo systemctl restart docker
```

### Local fallback registry

When harbor is offline, run a `registry:2` on the build host:

```bash
make registry-up              # starts registry:2 on :5000
echo "REGISTRY=192.168.1.51:5000/datawatch" > .env.build

# Configure docker daemon to allow plain-http registry
echo '{"insecure-registries":["192.168.1.51:5000"]}' | sudo tee /etc/docker/daemon.json
sudo systemctl restart docker
```

To make k8s nodes pull from the local registry, add the same
`insecure-registries` entry to `/etc/containerd/config.toml` on every
node and restart kubelet.

### Air-gapped (tarball)

```bash
LANGS="go kotlin" make container-tarball
ls dist/
# datawatch-agent-base-linux-amd64-v2.4.5.tar.xz
# datawatch-agent-claude-linux-amd64-v2.4.5.tar.xz
# datawatch-lang-go-linux-amd64-v2.4.5.tar.xz
# datawatch-lang-kotlin-linux-amd64-v2.4.5.tar.xz
# datawatch-parent-full-linux-amd64-v2.4.5.tar.xz

# On each cluster node:
sudo ctr -n=k8s.io images import datawatch-agent-base-linux-amd64-v2.4.5.tar.xz
sudo ctr -n=k8s.io images import datawatch-agent-claude-linux-amd64-v2.4.5.tar.xz
# … etc
```

## Cluster trust prerequisite

Before the K8s driver in F10 sprint 4 can spawn agents on a cluster, the
cluster's nodes must trust the registry's CA. This is the **single
biggest operator prerequisite** for cluster mode.

Symptoms when missing:
```
Failed to pull image "harbor.dmzs.com/datawatch/agent-base:v2.4.5":
  failed to do request: tls: failed to verify certificate:
  x509: certificate signed by unknown authority
```

Fix per cluster:
- **TKG/TKGI**: BOSH cluster manifest gains a `trusted_certificates` field;
  redeploy. Sprint 4 documents this in the Cluster Profile prerequisites.
- **kubeadm**: SSH every node, install CA via /etc/containerd/certs.d/
  layout above, restart kubelet.
- **k3s/microk8s**: their containerd configs live elsewhere; check distro docs.
- **Managed (GKE/EKS/AKS)**: each provider has a `trustedCertificates` or
  similar cluster-create option; can't be added after cluster creation
  in some.

## Pinning + upgrade

Every tool version in every Dockerfile is pinned via `ARG <NAME>=<VERSION>`
defaults. Reproducible builds at the cost of needing manual upgrade.

`scripts/container-upgrade.sh` queries upstream APIs (npm, GH releases,
PyPI, rubygems) for the latest of every pinned tool and rewrites the
ARG defaults in place:

```bash
make container-upgrade           # dry-run, prints diff
make container-upgrade APPLY=1   # rewrite files
git diff docker/dockerfiles/    # review
git commit -am "chore(F10): bump pinned container tool versions"
make container                   # rebuild + push
```

Project rule: rebuild + push every variant on every tagged release.

## Build engine — docker vs podman

Auto-detected. Override with `ENGINE=podman make container`. The
Makefile uses `$(ENGINE) buildx build` everywhere; podman 4+ aliases
`buildx` for compatibility, so the same recipe works.

## Continuous integration

`.github/workflows/container.yaml.disabled` is a stub that, when
renamed to `.yaml`, builds + pushes every variant to ghcr.io on tag
push. Disabled by default since the local Makefile is the primary path.
