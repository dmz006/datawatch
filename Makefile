BINARY=datawatch
VERSION=$(shell grep 'var Version' cmd/datawatch/main.go | head -1 | sed 's/.*"\(.*\)"/\1/')
BUILD_DIR=./bin
LDFLAGS=-X main.Version=$(VERSION) -X github.com/dmz006/datawatch/internal/server.Version=$(VERSION)

.PHONY: build clean install lint test fmt cross release release-snapshot channel-build \
        container container-load container-tarball container-clean \
        registry-up registry-down

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
CONTAINER_TAG   ?= $(VERSION)
SLIM_IMAGE      = $(REGISTRY)/datawatch:slim-$(CONTAINER_TAG)
FULL_IMAGE      = $(REGISTRY)/datawatch:full-$(CONTAINER_TAG)

# Container engine — auto-detect docker vs podman so option 3 (rootless
# podman / no root group membership) works with the same Makefile.
# Override with: make container ENGINE=podman
ENGINE ?= $(shell if command -v docker >/dev/null 2>&1; then echo docker; \
                  elif command -v podman >/dev/null 2>&1; then echo podman; \
                  else echo docker; fi)
# podman uses `podman build --jobs N` instead of buildx; same flag interface
# works for our use because podman aliases `buildx` for compatibility on
# recent versions (>= 4.0). For older podman, swap to `buildah bud`.
BUILD = $(ENGINE) buildx build

# Build slim + full multi-arch via buildx, push when PUSH=true (default true
# in .env.build for harbor; false for local dev to keep it fast).
container:
	@echo "→ building slim+full $(CONTAINER_TAG) for $(PLATFORMS) → $(REGISTRY)"
	@if [ "$(PUSH)" = "true" ]; then \
	    PUSH_ARG="--push"; \
	else \
	    PUSH_ARG="--load"; \
	    if echo "$(PLATFORMS)" | grep -q ","; then \
	        echo "[warn] PUSH=false forces single-arch (--load doesn't accept multi-arch)"; \
	        PLATFORMS_ARG="linux/$$(go env GOARCH)"; \
	    fi; \
	fi; \
	$(BUILD) \
	    --file Dockerfile.slim \
	    --platform $${PLATFORMS_ARG:-$(PLATFORMS)} \
	    --build-arg VERSION=$(VERSION) \
	    --tag $(SLIM_IMAGE) \
	    $$PUSH_ARG \
	    . && \
	$(BUILD) \
	    --file Dockerfile.full \
	    --platform $${PLATFORMS_ARG:-$(PLATFORMS)} \
	    --build-arg VERSION=$(VERSION) \
	    --tag $(FULL_IMAGE) \
	    $$PUSH_ARG \
	    .

# Single-arch dev build → loads into local docker daemon, no push.
container-load:
	@$(MAKE) container PUSH=false PLATFORMS=linux/$$(go env GOARCH)

# Air-gapped distribution: produce xz-compressed tarballs.
container-tarball:
	@mkdir -p dist
	@for arch in amd64 arm64; do \
	    for variant in slim full; do \
	        echo "→ tarball datawatch-$$variant-linux-$$arch-$(VERSION)"; \
	        docker buildx build \
	            --file Dockerfile.$$variant \
	            --platform linux/$$arch \
	            --build-arg VERSION=$(VERSION) \
	            --tag datawatch:$$variant-$(VERSION)-linux-$$arch \
	            --output type=docker,dest=- . \
	        | xz -T0 > dist/datawatch-$$variant-linux-$$arch-$(VERSION).tar.xz; \
	    done; \
	done

container-clean:
	docker buildx prune -af

# Local registry fallback for when harbor is unreachable / for air-gap dev.
# Plain HTTP, internal-only. Configure docker daemon to allow:
#   /etc/docker/daemon.json  →  { "insecure-registries": ["192.168.1.51:5000"] }
# For containerd/k8s nodes, see docs/container-build.md.
registry-up:
	docker run -d --restart=always -p 5000:5000 --name datawatch-registry registry:2 \
	    || docker start datawatch-registry

registry-down:
	-docker stop datawatch-registry
	-docker rm datawatch-registry

build:
	go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/datawatch/

install:
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

cross:
	mkdir -p $(BUILD_DIR)
	GOOS=linux   GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-amd64   ./cmd/datawatch/
	GOOS=linux   GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-arm64   ./cmd/datawatch/
	GOOS=darwin  GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-darwin-amd64  ./cmd/datawatch/
	GOOS=darwin  GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-darwin-arm64  ./cmd/datawatch/
	GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-windows-amd64.exe ./cmd/datawatch/

# Create a tagged release with pre-built binaries via GoReleaser.
# Tag the commit first: git tag vX.Y.Z && git push origin vX.Y.Z
# Then run: make release
release:
	goreleaser release --clean

# Build release artifacts locally without publishing (for testing).
release-snapshot:
	goreleaser release --snapshot --clean
