---
date: 2026-03-29
version: 0.7.3
scope: Replace signal-cli (Java) with libsignal (Rust FFI) via CGo bindings
status: planned
---

# Plan: libsignal Integration (Replace signal-cli)

## Problem

signal-cli requires Java 17+ (~200MB dependency), is slow to start, and adds operational complexity. libsignal (Rust) provides Signal's cryptographic primitives as a shared library with C FFI. However, libsignal is NOT a complete client — a full HTTP transport layer must be built.

## Current Interface (7 methods)

| Method | signal-cli RPC | Description |
|--------|---------------|-------------|
| Link | subprocess | Device linking via QR code |
| Send | `send` | Send message to group |
| Subscribe | `subscribeReceive` | Receive message notifications |
| ListGroups | `listGroups` | List joined groups |
| CreateGroup | `updateGroup` | Create new group |
| SelfNumber | in-memory | Return registered phone number |
| Close | stdin close | Shut down subprocess |

## Feature Gap Analysis

| Feature | signal-cli | libsignal FFI | Custom Code Needed |
|---------|-----------|---------------|-------------------|
| Link device | Full flow | Key agreement only | HTTP provisioning WebSocket, QR, finish-link |
| Send to group | Full | Encrypt only | HTTP PUT to Signal CDN, sender key distribution |
| Receive messages | Full | Decrypt only | WebSocket + envelope protobuf parsing |
| List groups | Full | No | HTTP GET + group credential presentation |
| Create group | Full | zkgroup primitives | HTTP + protobuf + credential presentation |
| Key storage | SQLite | No | Custom store implementation |

## Scope

- `internal/signal/libsignal/` — NEW CGo bindings
- `internal/signal/transport/` — NEW HTTP/WebSocket transport
- `internal/signal/native.go` — NEW NativeBackend implementing SignalBackend
- `internal/signal/factory.go` — NEW backend selection (signal-cli vs native)
- `scripts/libsignal-fetch.sh` — NEW build script for binary updates
- `install/install.sh` — update to conditionally skip Java
- `internal/signal/backend.go` — SignalBackend interface (KEEP STABLE)
- `internal/signal/signalcli.go` — current implementation (KEEP as fallback)

## Phases

### Phase 1 — Research & Build Infrastructure (Planned)

- Evaluate `../signal-go/` for existing Go Signal bindings
- `scripts/libsignal-fetch.sh`:
  - Downloads pre-built libsignal_ffi.so/dylib from Signal's GitHub releases
  - Verifies checksum, places in lib/
  - Checks for updates on each run
- `scripts/libsignal-test.sh`:
  - Runs functional test suite after library update
  - 100% code coverage gate
  - Fails build if signal functions broken

### Phase 2 — CGo Bindings (Planned)

- `internal/signal/libsignal/ffi.go` — CGo bridge to libsignal_ffi.h
- `internal/signal/libsignal/protocol.go` — encrypt/decrypt message wrappers
- `internal/signal/libsignal/groups.go` — group encrypt/decrypt (sender key)
- `internal/signal/libsignal/store.go` — ProtocolStore interface implementation
- Build tags: `//go:build cgo && !nosignal`

### Phase 3 — Signal Server HTTP Transport (Planned)

- `internal/signal/transport/client.go` — HTTP client for chat.signal.org, cert pinning
- `internal/signal/transport/websocket.go` — WSS message delivery, heartbeat, reconnect
- `internal/signal/transport/provisioning.go` — device linking protocol
- `internal/signal/transport/messages.go` — send/receive group messages
- `internal/signal/transport/groups.go` — list/create groups
- `internal/signal/transport/proto/` — protobuf definitions

### Phase 4 — NativeBackend (Planned)

- `internal/signal/native.go` — implements SignalBackend using Phase 2 + Phase 3
- Produces same `IncomingMessage` struct as `SignalCLIBackend`
- `internal/signal/factory.go` — `NewBackend(cfg)` returns signal-cli or native

### Phase 5 — Integration & Testing (Planned)

- Config switch: `signal.backend: "signal-cli" | "native"`
- Unit tests: CGo encrypt/decrypt round-trip
- Integration tests: link device, send message, receive message
- Feature parity tests: both backends produce identical results
- Update install scripts: add libsignal download, conditionally skip Java

### Phase 6 — Migration & Documentation (Planned)

- Keep signal-cli as default during transition
- Investigate reusing linked account data from signal-cli config dir
- Update docs/future-native-signal.md with status
- Update docs/messaging-backends.md Signal section

## Risk Assessment

- **Effort:** 3-6 months
- **Highest risk:** Signal server HTTP transport is undocumented, may change
- **Mitigation:** Keep signal-cli as permanent fallback; reference signal-cli Java source

## Dependencies

- `github.com/signalapp/libsignal` — pre-built binaries (Rust)
- `google.golang.org/protobuf` — message serialization
- CGo toolchain for linking

## Estimated Effort

3-6 months. Phase 1 (research) can begin immediately.
