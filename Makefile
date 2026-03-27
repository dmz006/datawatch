BINARY=datawatch
VERSION=0.5.4
BUILD_DIR=./bin

.PHONY: build clean install lint test fmt cross release release-snapshot

build:
	go build -ldflags="-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY) ./cmd/datawatch/

install:
	go install ./cmd/datawatch/

clean:
	rm -rf $(BUILD_DIR)

lint:
	golangci-lint run ./...

test:
	go test ./...

fmt:
	gofmt -w .

cross:
	mkdir -p $(BUILD_DIR)
	GOOS=linux   GOARCH=amd64 go build -ldflags="-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-linux-amd64   ./cmd/datawatch/
	GOOS=linux   GOARCH=arm64 go build -ldflags="-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-linux-arm64   ./cmd/datawatch/
	GOOS=darwin  GOARCH=amd64 go build -ldflags="-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-darwin-amd64  ./cmd/datawatch/
	GOOS=darwin  GOARCH=arm64 go build -ldflags="-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-darwin-arm64  ./cmd/datawatch/
	GOOS=windows GOARCH=amd64 go build -ldflags="-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-windows-amd64.exe ./cmd/datawatch/

# Create a tagged release with pre-built binaries via GoReleaser.
# Tag the commit first: git tag vX.Y.Z && git push origin vX.Y.Z
# Then run: make release
release:
	goreleaser release --clean

# Build release artifacts locally without publishing (for testing).
release-snapshot:
	goreleaser release --snapshot --clean
