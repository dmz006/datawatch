# Container hygiene runbook

This is the day-two operator runbook for the datawatch container image
inventory on GHCR. Covers: what's published, what's not, how to retag
patches manually, how to clean up the past-minor backlog. Companion to
[`scripts/delete-past-minor-assets.sh`](../scripts/delete-past-minor-assets.sh)
(release-asset retention) and [`scripts/delete-past-minor-containers.sh`](../scripts/delete-past-minor-containers.sh)
(GHCR retention — added v5.26.5).

## What CI publishes

Every `v*` tag push triggers `.github/workflows/containers.yaml`,
which builds and pushes:

| Image | Source | Notes |
|-------|--------|-------|
| `ghcr.io/dmz006/datawatch-agent-base` | `docker/dockerfiles/Dockerfile.agent-base` | Stage 1; also tagged at `ghcr.io/dmz006/datawatch/agent-base` (slash path) so Stage 2 `FROM ${REGISTRY}/agent-base` resolves |
| `ghcr.io/dmz006/datawatch-validator` | `docker/dockerfiles/Dockerfile.validator` | Stage 1 |
| `ghcr.io/dmz006/datawatch-stats-cluster` | `docker/dockerfiles/Dockerfile.stats-cluster` | Stage 1; also attached as a `.tar.gz` to the GH release for air-gapped installs |
| `ghcr.io/dmz006/datawatch-agent-claude` | `docker/dockerfiles/Dockerfile.agent-claude` | Stage 2 — FROM agent-base |
| `ghcr.io/dmz006/datawatch-agent-opencode` | `docker/dockerfiles/Dockerfile.agent-opencode` | Stage 2 |
| `ghcr.io/dmz006/datawatch-agent-aider` | `docker/dockerfiles/Dockerfile.agent-aider` | Stage 2 |
| `ghcr.io/dmz006/datawatch-agent-gemini` | `docker/dockerfiles/Dockerfile.agent-gemini` | Stage 2 |

Each gets two tags: `${VERSION}` (e.g. `v5.26.4`) and `latest`.

## What CI does NOT publish (gap → addressed in v6.0)

- **`parent-full`** — `docker/dockerfiles/Dockerfile.parent-full` exists
  and is referenced from `docs/howto/setup-and-install.md` (Option D —
  Container) but isn't in the CI matrix. The image `ghcr.io/dmz006/datawatch-parent-full`
  may be empty / stale on GHCR. v6.0 cut adds it as a Stage 2 entry
  (depends on agent-base, layered with signal-cli + Java runtime).
  Until then, operators who want this image build it locally:

  ```bash
  # From the repo root, after a fresh `git pull`:
  docker build \
    -f docker/dockerfiles/Dockerfile.parent-full \
    --build-arg REGISTRY=ghcr.io/dmz006/datawatch \
    --build-arg BASE_TAG=v5.26.4 \
    -t datawatch-parent-full:local \
    .

  docker run -it --rm \
    -v ~/.datawatch:/root/.datawatch \
    -p 8443:8443 -p 8080:8080 \
    datawatch-parent-full:local start
  ```

- **`agent-goose`** — Dockerfile not yet present; Goose is supported as
  a backend but only via host install. v6.0 cut will land the image.

## Retag for a patch (manual operator action)

The patch-only window between v5.x and v6.0 doesn't push fresh
binaries (`scripts/delete-past-minor-assets.sh` enforces this), but
containers continue to build because the workflow trigger is `tags: ['v*']`
and per the AGENT.md retention rule containers follow the same keep-set
as binaries (every major + latest minor + latest patch on latest minor).

If a patch needs to be retagged after the fact (e.g. CI failed and you
fixed it without bumping the version):

```bash
TAG=v5.26.4
for img in agent-base agent-claude agent-opencode agent-aider agent-gemini stats-cluster validator; do
  docker pull ghcr.io/dmz006/datawatch-${img}:${TAG}
  docker tag  ghcr.io/dmz006/datawatch-${img}:${TAG} ghcr.io/dmz006/datawatch-${img}:latest
  docker push ghcr.io/dmz006/datawatch-${img}:latest
done
# agent-base also under the slash path for Stage-2 chain consumers:
docker tag  ghcr.io/dmz006/datawatch-agent-base:${TAG} ghcr.io/dmz006/datawatch/agent-base:latest
docker push ghcr.io/dmz006/datawatch/agent-base:latest
```

## Cleanup — past-minor backlog

`scripts/delete-past-minor-containers.sh` deletes every GHCR image
version whose tag isn't in the keep-set. Same algorithm as
`delete-past-minor-assets.sh`: every major + latest minor + latest
patch on latest minor. Untagged dangling layers always pruned.

Requires a fine-grained PAT with `read:packages + delete:packages`
(the default `gh auth login` token is `read` only):

```bash
GITHUB_TOKEN=<pat> ./scripts/delete-past-minor-containers.sh
# Or DRY_RUN=1 to preview
DRY_RUN=1 GITHUB_TOKEN=<pat> ./scripts/delete-past-minor-containers.sh
```

The script iterates `datawatch-{parent-full, agent-base, agent-claude,
agent-opencode, agent-aider, agent-gemini, agent-goose, stats-cluster}`.
Skip-with-reason output explains each retained image.

## Vulnerability scanning (deferred — operator decision needed)

The Dockerfiles inherit from `debian:bookworm-slim` (parent / agent-base)
and `gcr.io/distroless/static` (stats-cluster). Routine scans:

```bash
# Trivy
trivy image --severity HIGH,CRITICAL ghcr.io/dmz006/datawatch-parent-full:latest

# grype (Anchore)
grype ghcr.io/dmz006/datawatch-parent-full:latest

# docker scout (built-in)
docker scout cves ghcr.io/dmz006/datawatch-parent-full:latest
```

Per AGENT.md release-discipline rule, this should run pre-release.
Currently it's manual — automation lands in v6.0 alongside the
`parent-full` CI add-on.

## Related documents

- [`scripts/delete-past-minor-assets.sh`](../scripts/delete-past-minor-assets.sh) — release-asset retention
- [`scripts/delete-past-minor-containers.sh`](../scripts/delete-past-minor-containers.sh) — GHCR container retention
- [`AGENT.md`](../AGENT.md) — § Release discipline rules — asset/container retention rule
- [`docs/howto/container-workers.md`](howto/container-workers.md) — operator-side container worker setup
- [`docs/security-review.md`](security-review.md) — pre-v6.0 security audit
