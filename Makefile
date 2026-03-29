BINARY=datawatch
VERSION=0.6.37
BUILD_DIR=./bin
LDFLAGS=-X main.Version=$(VERSION) -X github.com/dmz006/datawatch/internal/server.Version=$(VERSION)

.PHONY: build clean install lint test fmt cross release release-snapshot channel-build

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
