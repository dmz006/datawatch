# BL174 — image-size measurement (v2.4.5 → v4.7.0)

**Captured:** 2026-04-25 on the dev workstation (linux/amd64).
**Method:** `docker image inspect --format '{{.Size}}'` divided by
1 048 576 — uncompressed image size in MiB. Numbers represent the
total layered image size, not just the new layer's delta (which
`docker images` "DISK USAGE" misleadingly shows).

## Numbers

| Image | v2.4.5 baseline | v4.7.0 | Delta | Change driver |
|---|---|---|---|---|
| agent-base                 | 127 MB | 133 MB | **+6 MB** | datawatch-channel Go binary bundled (BL174 part 2, v4.4.0) + datawatch binary growth across BL170/172/173/174/S13 |
| agent-claude               | 199 MB | 205 MB | **+6 MB** | inherits agent-base; claude.exe layout unchanged |
| **agent-opencode**         | 232 MB | **182 MB** | **−50 MB (−22%)** | nodejs removed from runtime (BL174 v4.6.0); native opencode binary pulled directly from per-platform npm tarball |
| **datawatch-stats-cluster** *(new in v4.5.0)* | n/a | **11 MB** | — | distroless cc-debian12:nonroot multi-stage; just the Go binary |

## What this confirms

- **BL174's biggest size win was agent-opencode**, not agent-claude.
  agent-claude's v2.4.5 build already used a multi-stage build that
  copied only `claude.exe` out of the nodejs builder; v4.6.0's switch
  to per-platform tarballs in the builder made the build faster + more
  hermetic but didn't change the runtime image. agent-opencode was
  the one that previously installed nodejs at runtime to wrap a
  native binary; v4.6.0 cut that dependency entirely.
- **agent-base growth (+6 MB) is the channel-bridge bundle plus the
  daemon binary growth across 8 sprints**. Net: every downstream
  agent image grew 6 MB versus v2.4.5 — a fair tradeoff for the
  BL174 / BL172 / BL173 / S13 features added in that span.
- **datawatch-stats-cluster (Shape C) at 11 MB validates the
  distroless choice in BL173**. The Helm DaemonSet pulls a tiny
  image per node — usually under 1 s on a warm registry.

## What's not measured here

- **Compressed (transport) size**. `docker save | wc -c` would give
  the on-the-wire size. Likely 30-40% smaller than these numbers.
  Not blocking for the BL174 close-out.
- **Runtime memory footprint**. Image size != RSS. Runtime
  measurements live in observer envelopes; spot-check via
  `/api/observer/peers/<peer>/stats` once a real Shape C pod is
  running.

## Reproduction

```bash
sg docker -c "make container-agent-base       REGISTRY=local-test"
sg docker -c "make container-agent-claude     REGISTRY=local-test"
sg docker -c "make container-agent-opencode   REGISTRY=local-test"
sg docker -c "docker buildx build -f docker/dockerfiles/Dockerfile.stats-cluster --platform linux/amd64 --build-arg VERSION=4.7.0 --tag local-test/datawatch-stats-cluster:v4.7.0 --load ."

for img in local-test/agent-base:v4.7.0 local-test/agent-claude:v4.7.0 \
           local-test/agent-opencode:v4.7.0 local-test/datawatch-stats-cluster:v4.7.0; do
  printf "%-50s %s MB\n" "$img" \
    "$(sg docker -c "docker image inspect --format '{{.Size}}' $img" | awk '{printf "%.0f", $1/1048576}')"
done
```

## Closes

- BL174 image-size measurement task from the v4.6.0 follow-up list.
- Confirms BL174's slim-claude-container goal: agent-claude v4.7.0 is
  205 MB (vs e.g. the official `node:lts-bookworm` at ~430 MB if we
  had to ship node).
