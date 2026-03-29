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

## Device Linking (QR Code) Workflow

The full link flow must be replicated:

1. **Generate ephemeral key pair** (X25519) — libsignal provides this
2. **Open provisioning WebSocket** to `wss://chat.signal.org/v1/provisioning/`
3. **Receive ProvisioningUUID** from server — encode as `sgnl://linkdevice?uuid=<UUID>&pub_key=<base64>`
4. **Display QR code** using existing `qrterminal` library (same as current flow)
5. **Wait for user to scan** on primary device (Signal app → Linked Devices → Link New Device)
6. **Receive ProvisionMessage** on WebSocket — encrypted with our ephemeral public key
7. **Decrypt** using libsignal's key agreement → get identity key pair, phone number, provisioning code
8. **Register as linked device** via HTTP PUT to `/v1/devices/<provisioningCode>`
9. **Generate and upload prekeys** (signed prekey, one-time prekeys) via HTTP
10. **Store credentials** in local key store

**Risk:** Steps 2-8 use Signal's undocumented provisioning protocol. The wire format is protobuf. signal-cli Java source is the reference implementation.

## Feature Feasibility Assessment

| Feature | libsignal Provides | Custom Code | Feasible? | Risk |
|---------|-------------------|-------------|-----------|------|
| **Device linking (QR)** | Key agreement, encryption | WebSocket provisioning, HTTP registration | **YES** | High — undocumented protocol |
| **Send group message** | Sender key encrypt, sealed sender | HTTP PUT, sender key distribution | **YES** | Medium — need protobuf format |
| **Receive messages** | Decrypt envelope, unsealed sender | WebSocket receive, envelope parsing | **YES** | Medium — need protobuf format |
| **List groups** | zkgroup credentials | HTTP GET with auth | **YES** | Medium — credential presentation |
| **Create group** | zkgroup create, auth credential | HTTP + group creation protobuf | **YES** | High — complex zkgroup flow |
| **Read receipts** | N/A | HTTP POST (simple) | **YES** | Low |
| **Typing indicators** | N/A | WebSocket (simple) | **YES** | Low |
| **Profile/avatar** | Profile key encrypt | HTTP GET/PUT | **Partial** | Low — not needed for datawatch |
| **Disappearing messages** | Timer handling | Message expiry logic | **Partial** | Low — not critical |
| **Stickers/attachments** | Attachment encrypt | CDN upload/download | **NO** — not needed | N/A |
| **Voice/video calls** | N/A | N/A | **NO** — out of scope | N/A |

### Features that WILL NOT work or are NOT planned:
- **Stickers, attachments, media** — datawatch is text-only, no need
- **Voice/video calls** — completely out of scope
- **Stories** — not relevant
- **Payment** — not relevant

### Features with HIGH implementation risk:
- **Device linking** — undocumented provisioning WebSocket protocol
- **Group creation** — requires zkgroup credential presentation (complex crypto)
- **Sender key distribution** — must send SenderKeyDistributionMessage to all group members before first message

## Risk Assessment

- **Effort:** 3-6 months
- **Highest risk:** Signal server HTTP transport is undocumented, may change without notice
- **Second risk:** Device linking protocol is complex and must match exactly
- **Mitigation:** Keep signal-cli as permanent fallback; reference signal-cli Java source
- **Go/No-Go decision:** After Phase 1 (research), evaluate if the effort is justified
  given that signal-cli works. If device linking can't be replicated, the entire plan is blocked.

## Dependencies

- `github.com/signalapp/libsignal` — pre-built binaries (Rust)
- `google.golang.org/protobuf` — message serialization
- CGo toolchain for linking

## Estimated Effort

3-6 months. Phase 1 (research) can begin immediately.
