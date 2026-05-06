# datawatch v5.26.42 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.41 → v5.26.42
**Patch release** (no binaries — operator directive).
**Closed:** `Dockerfile.agent-goose` written + wired into the CI publish path (CI residual from v5.26.25 audit).

## What's new

### `agent-goose` joins the publishable agent images

The `goose` LLM backend has lived in `internal/llm/backends/goose/` for a while (Block's `goose run --text` invocation pattern), but its corresponding container image was placeholder-only — operators picking `image_pair.agent: agent-goose` in a project profile had no published image to pull. v5.26.42 closes that gap.

`docker/dockerfiles/Dockerfile.agent-goose`:

```dockerfile
FROM ${REGISTRY}/agent-base:${BASE_TAG} AS runtime
ARG GOOSE_VERSION=1.32.0   # default bumps with each release pass

USER root
RUN ARCH="$TARGETARCH"; case "$ARCH" in
        amd64) GOOSE_ARCH="x86_64-unknown-linux-gnu" ;;
        arm64) GOOSE_ARCH="aarch64-unknown-linux-gnu" ;;
    esac && \
    curl -fsSL "https://github.com/block/goose/releases/download/v${GOOSE_VERSION}/goose-${GOOSE_ARCH}.tar.bz2" \
        -o /tmp/goose.tar.bz2 && \
    install_packages bzip2 && \
    tar -xjf /tmp/goose.tar.bz2 -C /usr/local/bin/ && \
    chmod +x /usr/local/bin/goose && \
    rm /tmp/goose.tar.bz2 && \
    rm -rf /var/cache/apt/* /tmp/* /root/.cache

USER datawatch
LABEL datawatch.variant="agent-goose" datawatch.tools.goose=$GOOSE_VERSION
```

Single-stage (the binary is already self-contained Rust, no toolchain needed at runtime). The `bzip2` package gets installed late and the cache cleanup wipes it from the apt index, so the layer adds only the binary itself + bzip2's small unpacked footprint.

### CI publish wiring

`containers.yaml` stage 2 build matrix gained an `agent-goose` row alongside `agent-claude` / `agent-opencode` / `agent-aider` / `agent-gemini` / `parent-full`. After v5.26.42 tags, GHCR will carry:

```
ghcr.io/dmz006/datawatch-agent-goose:5.26.42
ghcr.io/dmz006/datawatch-agent-goose:latest
```

Operator-tunable via the standard build-args:

```bash
docker build \
  --build-arg GOOSE_VERSION=1.32.0 \
  -f docker/dockerfiles/Dockerfile.agent-goose .
```

### Verification

URL pattern + tarball layout validated locally before commit:

```bash
$ gh api repos/block/goose/releases/latest --jq '.tag_name'
v1.32.0
$ curl -fsSL https://github.com/block/goose/releases/download/v1.32.0/goose-x86_64-unknown-linux-gnu.tar.bz2 \
    | tar -tjf -
./
./goose
$ /tmp/extracted/goose --version
1.32.0
```

The tarball uses a `./goose` entry rather than bare `goose`, so the `tar` invocation extracts everything (only one file in the archive) instead of using the bare-name positional that would miss the leading `./`. A defensive `test -x` post-check fails the build loudly if the binary is missing after extraction.

## Configuration parity

No new config knob. Operators select `agent-goose` via a Project Profile's `image_pair.agent` field.

## Tests

Image build itself runs in CI on the v5.26.42 tag push. Local Go test suite unaffected (still 465 passing). Smoke unaffected (40/0/1).

## Known follow-ups

CI residuals after v5.26.42:

- **Kind-cluster smoke workflow** — spin up `kind`, deploy chart, run `release-smoke.sh`. Last big remaining CI item before v6.0.

Phase 6 screenshot recapture, Phase 3 + Phase 4 design, mempalace alignment audit, service-function smoke completeness — all unchanged.

## Upgrade path

```bash
git pull
# No daemon restart needed — Dockerfile + workflow change only.
# After the v5.26.42 tag push completes, pull the published image:
#
#   docker pull ghcr.io/dmz006/datawatch-agent-goose:5.26.42
#
# or reference it in a Project Profile's image_pair.agent field
# and let cluster-spawn fetch it on next worker spawn.
```
