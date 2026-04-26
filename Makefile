BINARY=datawatch
VERSION=$(shell grep 'var Version' cmd/datawatch/main.go | head -1 | sed 's/.*"\(.*\)"/\1/')
BUILD_DIR=./bin
LDFLAGS=-X main.Version=$(VERSION) -X github.com/dmz006/datawatch/internal/server.Version=$(VERSION)

.PHONY: build clean install lint test fmt cross release release-snapshot channel-build \
        container container-load container-tarball container-clean container-upgrade \
        container-agent-base container-parent-full _container-build \
        registry-up registry-down sync-docs

# v4.0.4 — sync docs/ into internal/server/web/docs/ so the embedded
# web FS carries the markdown files the in-PWA diagram viewer
# renders. Run before every `build`, `cross`, and container build.
sync-docs:
	@rsync -a --delete --include='*/' --include='*.md' --exclude='*' docs/ internal/server/web/docs/

# ── F10: container build pipeline ─────────────────────────────────────────
# Variables read from .env.build (gitignored) so the IP/registry never lives
# in committed source. .env.build.example documents what to set.
ifneq (,$(wildcard ./.env.build))
    include .env.build
    export
endif

# Defaults if .env.build is absent (safe for `make container-load` dev loop).
REGISTRY        ?= localhost:5000/datawatch
PLATFORMS       ?= linux/amd64,linux/arm64
PUSH            ?= false
CONTAINER_TAG   ?= v$(VERSION)

# Image taxonomy (S1.9 — per-agent + per-language):
#   agent-base    — minimal foundation (datawatch + tmux + git + gh + rtk)
#                   ~250-300MB; no node, no python, no agents, no langs
#   agent-{claude,opencode,gemini,aider}
#                   — one LLM agent per image, FROM agent-base
#   lang-{go,node,python,rust,kotlin}
#                   — one language toolchain per image, FROM agent-base
#   parent-full   — control-plane (agent-base + signal-cli + JRE)
#
# Composition: deploy ONE agent-* container + ONE lang-* container in
# the same Pod, sharing /workspace. K8s driver (sprint 4) builds the
# Pod manifest from a Project Profile's {agent, language} tuple.
#
# Override to a subset for fast iteration:
#   AGENT_TYPES="claude" LANG_TYPES="go" make container
AGENT_TYPES ?= claude opencode gemini aider
LANG_TYPES  ?= go node python rust kotlin ruby
# tools-* images pair with an agent-* for non-coding work
# (ops/infra, data, docs, etc). See docs/composition-examples.md.
TOOL_TYPES  ?= ops

# Container engine — auto-detect docker vs podman so option 3 (rootless
# podman / no root group membership) works with the same Makefile.
# Override with: make container ENGINE=podman
ENGINE ?= $(shell if command -v docker >/dev/null 2>&1; then echo docker; \
                  elif command -v podman >/dev/null 2>&1; then echo podman; \
                  else echo docker; fi)
BUILD = $(ENGINE) buildx build

# All agent variants depend on agent-base; the Dockerfiles read REGISTRY +
# BASE_TAG via build-args so we can stack them on whatever's already in
# the registry without a hard-coded registry.example.com.
COMMON_BUILDARGS = --build-arg VERSION=$(VERSION) \
                   --build-arg REGISTRY=$(REGISTRY) \
                   --build-arg BASE_TAG=$(CONTAINER_TAG)

# Path-resolution helper: each variant's Dockerfile lives under
# docker/dockerfiles/Dockerfile.<variant>. Build context stays at repo root.
DOCKERFILE = docker/dockerfiles/Dockerfile.$(1)

# ── Top-level: build everything ────────────────────────────────────────
# Builds the dependency chain in order. agent-base must be in the
# registry (or local daemon) before any agent-*/lang-* can FROM it.
container: container-agent-base \
           $(addprefix container-agent-,$(AGENT_TYPES)) \
           $(addprefix container-lang-,$(LANG_TYPES)) \
           $(addprefix container-tools-,$(TOOL_TYPES)) \
           container-parent-full
	@echo "→ all variants built and (if PUSH=true) pushed at $(CONTAINER_TAG)"

# Single-arch dev build of agent-base only → loads to local docker.
# Useful for iterating on agent-base without paying multi-arch + push cost.
container-load:
	@$(MAKE) container-agent-base PUSH=false PLATFORMS=linux/$$(go env GOARCH)

# Per-variant targets — all reach a single underlying recipe.
container-agent-base:
	@$(MAKE) _container-build VARIANT=agent-base

container-agent-%:
	@$(MAKE) _container-build VARIANT=agent-$*

container-lang-%:
	@$(MAKE) _container-build VARIANT=lang-$*

container-tools-%:
	@$(MAKE) _container-build VARIANT=tools-$*

container-parent-full:
	@$(MAKE) _container-build VARIANT=parent-full

# ── Internal recipe: build one variant ─────────────────────────────────
_container-build:
	@if [ -z "$(VARIANT)" ]; then echo "VARIANT must be set"; exit 1; fi
	@echo "→ building $(VARIANT)  $(REGISTRY)/$(VARIANT):$(CONTAINER_TAG)"
	@if [ "$(PUSH)" = "true" ]; then \
	    PUSH_ARG="--push"; \
	    PLATFORMS_USE="$(PLATFORMS)"; \
	    CACHE_ARGS="--cache-from type=registry,ref=$(REGISTRY)/$(VARIANT):buildcache --cache-to type=registry,ref=$(REGISTRY)/$(VARIANT):buildcache,mode=max"; \
	else \
	    PUSH_ARG="--load"; \
	    PLATFORMS_USE="linux/$$(go env GOARCH)"; \
	    CACHE_ARGS=""; \
	    if [ "$(PLATFORMS)" != "$$PLATFORMS_USE" ]; then \
	        echo "[info] PUSH=false: forcing single-arch ($$PLATFORMS_USE), no registry cache"; \
	    fi; \
	fi; \
	$(BUILD) \
	    --file docker/dockerfiles/Dockerfile.$(VARIANT) \
	    --platform $$PLATFORMS_USE \
	    $(COMMON_BUILDARGS) \
	    --tag $(REGISTRY)/$(VARIANT):$(CONTAINER_TAG) \
	    --tag $(REGISTRY)/$(VARIANT):latest \
	    $$CACHE_ARGS \
	    $$PUSH_ARG \
	    .

# Air-gapped distribution: tarball the agent-base + a chosen language variant.
# Override LANGS at the command line to pick what to ship: make container-tarball LANGS="go kotlin".
container-tarball: LANGS ?= go
container-tarball:
	@mkdir -p dist
	@for arch in amd64 arm64; do \
	    for variant in agent-base $(addprefix agent-,$(LANGS)) parent-full; do \
	        echo "→ tarball datawatch-$$variant-linux-$$arch-$(CONTAINER_TAG)"; \
	        $(BUILD) \
	            --file docker/dockerfiles/Dockerfile.$$variant \
	            --platform linux/$$arch \
	            $(COMMON_BUILDARGS) \
	            --tag datawatch:$$variant-$(CONTAINER_TAG)-linux-$$arch \
	            --output type=docker,dest=- . \
	        | xz -T0 > dist/datawatch-$$variant-linux-$$arch-$(CONTAINER_TAG).tar.xz; \
	    done; \
	done

container-clean:
	$(ENGINE) buildx prune -af

# ── Upgrade: bump pinned tool versions to upstream latest ─────────────
# Reads latest versions from upstream APIs and rewrites the ARG defaults
# in every Dockerfile under docker/dockerfiles/. Print-only by default;
# set APPLY=1 to actually rewrite.
container-upgrade:
	@scripts/container-upgrade.sh $(if $(APPLY),--apply,)

# Local registry fallback for when harbor is unreachable / for air-gap dev.
# Plain HTTP, internal-only. Configure docker daemon to allow:
#   /etc/docker/daemon.json  →  { "insecure-registries": ["198.51.100.10:5000"] }
# For containerd/k8s nodes, see docs/container-build.md.
registry-up:
	docker run -d --restart=always -p 5000:5000 --name datawatch-registry registry:2 \
	    || docker start datawatch-registry

registry-down:
	-docker stop datawatch-registry
	-docker rm datawatch-registry

build: sync-docs
	go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/datawatch/

install: sync-docs
	go build -ldflags="$(LDFLAGS)" -o $(HOME)/.local/bin/$(BINARY) ./cmd/datawatch/

clean:
	rm -rf $(BUILD_DIR)

lint:
	golangci-lint run ./...

test:
	go test ./...

fmt:
	gofmt -w .

# Rebuild the MCP channel TypeScript and copy the output into the Go embed path.
# Run this after editing channel/index.ts, then rebuild/install datawatch.
channel-build:
	cd channel && node_modules/.bin/tsc
	cp channel/dist/index.js internal/channel/embed/channel.js

cross: sync-docs
	mkdir -p $(BUILD_DIR)
	# -trimpath + -s -w strips debug info and absolute build paths;
	# typically 30-40% smaller binary at zero runtime cost.
	GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags="-s -w $(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-amd64   ./cmd/datawatch/
	GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags="-s -w $(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-arm64   ./cmd/datawatch/
	GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags="-s -w $(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-darwin-amd64  ./cmd/datawatch/
	GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags="-s -w $(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-darwin-arm64  ./cmd/datawatch/
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w $(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-windows-amd64.exe ./cmd/datawatch/
	$(MAKE) cross-agent
	# Opt-in UPX pack — runs only if upx is on PATH. Linux + Windows
	# only (UPX has known issues with macOS Mach-O binaries on recent
	# OS versions). --best gives the largest reduction; --lzma is
	# slower but tighter. Skipping macOS keeps darwin builds notarized-
	# friendly. Failure to pack any single binary is non-fatal.
	@if command -v upx >/dev/null 2>&1; then \
		echo ">>> upx present — packing release binaries (linux + windows only)"; \
		upx --best --lzma $(BUILD_DIR)/$(BINARY)-linux-amd64       2>/dev/null || true; \
		upx --best --lzma $(BUILD_DIR)/$(BINARY)-linux-arm64       2>/dev/null || true; \
		upx --best --lzma $(BUILD_DIR)/$(BINARY)-windows-amd64.exe 2>/dev/null || true; \
	else \
		echo ">>> upx not on PATH — skipping pack step (install upx for ~50% smaller release binaries)"; \
	fi

# BL86 — datawatch-agent stats binary (linux only — relies on
# /proc + nvidia-smi + free + df).
cross-agent:
	GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/datawatch-agent-linux-amd64 ./cmd/datawatch-agent/
	GOOS=linux GOARCH=arm64 go build -o $(BUILD_DIR)/datawatch-agent-linux-arm64 ./cmd/datawatch-agent/

# BL173 task 1 — generate the eBPF objects via bpf2go. Requires clang +
# kernel headers (linux-headers-$(uname -r) on Debian/Ubuntu). Without
# this step the netprobe loader degrades to noop with a clear message.
ebpf-gen:
	cd internal/observer/ebpf && go generate ./...

# BL173 task 4 — build the Shape C cluster container image. Multi-arch
# via buildx; pushes to ghcr.io/dmz006/datawatch-stats-cluster:$(VERSION).
# REGISTRY can be overridden for local registry pushes.
REGISTRY ?= ghcr.io/dmz006
cluster-image:
	docker buildx build --platform linux/amd64,linux/arm64 \
	    -f docker/dockerfiles/Dockerfile.stats-cluster \
	    -t $(REGISTRY)/datawatch-stats-cluster:$(VERSION) \
	    -t $(REGISTRY)/datawatch-stats-cluster:latest \
	    --build-arg VERSION=$(VERSION) \
	    --push .

# BL172 (S11) — Shape B standalone observer daemon. Cross-build all
# platforms; ships as release artifacts so operators can drop the matching
# binary on Ollama / GPU / mobile-edge boxes.
cross-stats:
	mkdir -p $(BUILD_DIR)
	GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.Version=$(VERSION)" -o $(BUILD_DIR)/datawatch-stats-linux-amd64       ./cmd/datawatch-stats/
	GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags="-s -w -X main.Version=$(VERSION)" -o $(BUILD_DIR)/datawatch-stats-linux-arm64       ./cmd/datawatch-stats/
	GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.Version=$(VERSION)" -o $(BUILD_DIR)/datawatch-stats-darwin-amd64      ./cmd/datawatch-stats/
	GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags="-s -w -X main.Version=$(VERSION)" -o $(BUILD_DIR)/datawatch-stats-darwin-arm64      ./cmd/datawatch-stats/
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.Version=$(VERSION)" -o $(BUILD_DIR)/datawatch-stats-windows-amd64.exe ./cmd/datawatch-stats/

# BL174 — native Go MCP channel bridge. Cross-build for every platform
# the parent supports so operators can drop the matching binary next
# to `datawatch` and skip the Node.js dependency for channel mode.
cross-channel:
	mkdir -p $(BUILD_DIR)
	GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o $(BUILD_DIR)/datawatch-channel-linux-amd64       ./cmd/datawatch-channel/
	GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o $(BUILD_DIR)/datawatch-channel-linux-arm64       ./cmd/datawatch-channel/
	GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o $(BUILD_DIR)/datawatch-channel-darwin-amd64      ./cmd/datawatch-channel/
	GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o $(BUILD_DIR)/datawatch-channel-darwin-arm64      ./cmd/datawatch-channel/
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o $(BUILD_DIR)/datawatch-channel-windows-amd64.exe ./cmd/datawatch-channel/

# Create a tagged release with pre-built binaries via GoReleaser.
# Tag the commit first: git tag vX.Y.Z && git push origin vX.Y.Z
# Then run: make release
release:
	goreleaser release --clean

# Build release artifacts locally without publishing (for testing).
release-snapshot:
	goreleaser release --snapshot --clean
