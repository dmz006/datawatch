# Future: Native Go Signal Implementation

## Current Approach: signal-cli

`claude-signal` currently uses [signal-cli](https://github.com/AsamK/signal-cli) as a subprocess, communicating over JSON-RPC 2.0 via stdin/stdout.

### Why signal-cli?

Signal is a proprietary protocol with no official specification. The official implementation lives in:
- **[libsignal](https://github.com/signalapp/libsignal)** — Signal's core cryptographic library, written in Rust
- **Java/Android/iOS clients** — Official Signal clients
- **signal-cli** — A community-maintained Java CLI that uses Signal's official Java bindings to libsignal

Using signal-cli means we inherit:
- The official libsignal cryptographic implementation
- Support for Signal Group V2 (which uses zero-knowledge proofs from zkgroup)
- Compatibility with Signal protocol updates
- A maintained wrapper maintained by the community

### Trade-offs

| | signal-cli | Native Go |
|---|---|---|
| Dependencies | Java 17+, signal-cli binary | None (or libsignal CGo) |
| Protocol correctness | Official library | Would need to implement or bind |
| Maintenance | Community-maintained | Self-maintained |
| Performance | Subprocess overhead | In-process |
| Deployment | Two binaries | Single binary |

---

## Current State of Go Signal Implementations (as of 2024)

There is no production-ready, actively maintained pure Go implementation of the Signal protocol as of 2024. Existing projects:

- **[textsecure](https://github.com/signal-golang/textsecure)** — Unmaintained since 2020; does not support Group V2 or modern Signal features
- **[go-signal](https://github.com/WireGuard/wireguard-go)** — Not related (WireGuard)
- **Various forks** — None have kept up with Signal's protocol evolution

The core challenge is Signal Group V2, which relies on **zkgroup** — a complex zero-knowledge proof system for anonymous group credentials. Implementing this correctly and keeping it current requires significant ongoing engineering effort.

---

## The `SignalBackend` Interface Design

The entire `claude-signal` architecture is designed around the `SignalBackend` interface:

```go
type SignalBackend interface {
    Link(deviceName string, onQR func(qrURI string)) error
    Send(groupID, message string) error
    Subscribe(ctx context.Context, handler func(IncomingMessage)) error
    ListGroups(ctx context.Context) ([]Group, error)
    SelfNumber() string
    Close() error
}
```

This interface is the sole coupling point between the Signal layer and the rest of the application. Swapping backends requires:
1. Implementing the interface
2. Changing one line in `main.go` where `NewSignalCLIBackend` is called

The router, session manager, config, and CLI commands are entirely backend-agnostic.

---

## What a Native Go Implementation Would Require

### Option A: libsignal-ffi via CGo

libsignal exposes a C Foreign Function Interface (FFI):
- Repository: https://github.com/signalapp/libsignal
- The FFI is documented in `libsignal-ffi/src/lib.rs`
- Signal's Swift and Node.js clients use this FFI

**Steps:**
1. Build `libsignal_ffi.a` from source (Rust + Cargo)
2. Write CGo bindings in Go (`import "C"` with the header from signal-ffi)
3. Implement the `SignalBackend` interface using those bindings
4. Handle the complexity of account registration, key generation, sealed sender, etc.

**Challenges:**
- libsignal-ffi API changes with Signal protocol updates
- CGo adds build complexity and disables some Go tooling
- Cross-compilation becomes harder with a CGo dependency
- Must ship the compiled `.a` library or build it as part of the Go build

### Option B: Pure Go Signal Protocol

**Would require implementing:**
- X3DH key agreement (Extended Triple Diffie-Hellman)
- Double Ratchet Algorithm for message encryption
- Signal's sealed sender mechanism
- Signal Group V2 with zkgroup zero-knowledge proofs
- Signal's protobuf message format
- Device registration and linking flow
- Message transport over Signal's WebSocket API

**Relevant references:**
- [Signal Protocol specification](https://signal.org/docs/)
- [libsignal source](https://github.com/signalapp/libsignal)
- [signal-cli source](https://github.com/AsamK/signal-cli) — Java implementation to study

### Option C: Embedded JVM / GraalVM Native Image

An alternative path is to compile signal-cli into a native binary using GraalVM Native Image, then embed it (or link against the native library). This would eliminate the Java runtime dependency while still using the official implementation.

---

## Recommended Path Forward

1. **Short term (now):** Continue using `SignalCLIBackend`. It works, is reliable, and uses the official Signal library.

2. **Medium term:** Monitor the ecosystem for a production-ready Go Signal library. The Signal protocol has been relatively stable since Group V2 was deployed.

3. **Long term:** If a suitable libsignal-ffi Go binding emerges (perhaps from the broader Go ecosystem or from Signal itself), implement `LibSignalBackend` using the interface. Existing users would see no API changes.

---

## Links

- libsignal repository: https://github.com/signalapp/libsignal
- libsignal C FFI: https://github.com/signalapp/libsignal/tree/main/swift/Sources/LibSignalClient (FFI used by Swift client, similar pattern for Go)
- signal-cli repository: https://github.com/AsamK/signal-cli
- Signal Protocol documentation: https://signal.org/docs/
- zkgroup paper: https://eprint.iacr.org/2019/1416.pdf
