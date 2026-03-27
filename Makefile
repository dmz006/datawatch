BINARY=datawatch
VERSION=0.1.0
BUILD_DIR=./bin

.PHONY: build clean install lint test fmt cross

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
	GOOS=linux  GOARCH=amd64 go build -ldflags="-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-linux-amd64  ./cmd/datawatch/
	GOOS=linux  GOARCH=arm64 go build -ldflags="-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-linux-arm64  ./cmd/datawatch/
	GOOS=darwin GOARCH=amd64 go build -ldflags="-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-darwin-amd64 ./cmd/datawatch/
	GOOS=darwin GOARCH=arm64 go build -ldflags="-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-darwin-arm64 ./cmd/datawatch/
