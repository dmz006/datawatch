# OSS Scanner build image for datawatch (DRAFT — not yet validated).
#
# Scan-time contract: the scanner runs this image with NO internet access.
# Everything needed to build and test must be fetched here, at image build time.
# Module downloads happen in the build stage; the final test step runs with
# GOPROXY=off and GOFLAGS=-mod=readonly so any missing dependency fails loudly.

FROM golang:1.26.9-bookworm

# tmux and git are used by some datawatch tests; nodejs/npm are needed by
# internal/channel, which extracts and npm-installs the MCP channel server in tests.
# make and rsync run `make docs-index`, which generates two gitignored files that
# go:embed needs (internal/docsindex/assets, internal/server/web/docs).
RUN apt-get update && \
    apt-get install -y --no-install-recommends git tmux ca-certificates nodejs npm make rsync && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Fetch all modules at build time (network allowed here).
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy the source, then generate the gitignored files that go:embed needs.
COPY . .
RUN make docs-index
RUN GOFLAGS=-mod=readonly go build ./... && \
    GOFLAGS=-mod=readonly go vet ./...

# Build-time test gate: maintainers confirm the suite passes inside this image.
# This run also warms the npm cache that internal/channel's tests need offline.
RUN GOFLAGS=-mod=readonly go test ./...

# Scan-time environment: no network.
ENV GOPROXY=off \
    GOFLAGS=-mod=readonly \
    GOTOOLCHAIN=local \
    NPM_CONFIG_OFFLINE=true

CMD ["go", "test", "-count=1", "./..."]
