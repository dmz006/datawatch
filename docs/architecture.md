# Architecture

## Component Overview

`claude-signal` is composed of four main packages plus the CLI entry point:

| Package | Path | Role |
|---|---|---|
| `config` | `internal/config` | Load, validate, and save YAML configuration |
| `signal` | `internal/signal` | Signal protocol abstraction (backend interface + signal-cli implementation) |
| `session` | `internal/session` | Session lifecycle, tmux management, persistent store |
| `router` | `internal/router` | Message parsing and command dispatch |
| `main` | `cmd/claude-signal` | CLI entry point (cobra commands) |

---

## Component Diagram

```mermaid
graph TD
    subgraph "Signal Infrastructure"
        Phone["Signal Mobile App"]
        Group["Signal Group\n(shared control channel)"]
        SignalCLI["signal-cli\n(Java subprocess)"]
    end

    subgraph "claude-signal daemon"
        Backend["SignalCLIBackend\nimplements SignalBackend"]
        Router["Router\ncommand dispatch"]
        Manager["Session Manager"]
        Store["Store\n(sessions.json)"]
        Tmux["TmuxManager"]
    end

    subgraph "claude-code sessions"
        S1["tmux: cs-host-a3f2\n→ claude 'task A'"]
        S2["tmux: cs-host-b7c1\n→ claude 'task B'"]
        L1["log: host-a3f2.log"]
        L2["log: host-b7c1.log"]
    end

    Phone -->|group message| Group
    Group <-->|JSON-RPC stdin/stdout| SignalCLI
    SignalCLI <-->|subprocess| Backend
    Backend --> Router
    Router --> Manager
    Manager --> Store
    Manager --> Tmux
    Tmux --> S1
    Tmux --> S2
    S1 --> L1
    S2 --> L2
    Manager -->|monitor goroutines| L1
    Manager -->|monitor goroutines| L2
    Router -->|send reply| Backend
    Backend -->|send| SignalCLI
```

---

## SignalBackend Interface

The `SignalBackend` interface (in `internal/signal/backend.go`) decouples the Signal protocol implementation from the rest of the application:

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

**Current implementation:** `SignalCLIBackend` — runs `signal-cli` as a child process in `jsonRpc` mode, communicating over stdin/stdout with newline-delimited JSON-RPC 2.0 messages.

**Future implementation:** A native Go backend using libsignal-ffi bindings (see `docs/future-native-signal.md`).

This interface is the primary extension point of the application. Swapping backends requires no changes to the router, session manager, or CLI.

---

## Session Lifecycle State Machine

```mermaid
stateDiagram-v2
    [*] --> running : Start() called\ntmux + claude-code created

    running --> waiting_input : Monitor detects idle prompt\n(output ends with ?, >, [y/N], etc.)
    waiting_input --> running : SendInput() called\nor new output detected

    running --> complete : tmux session exits cleanly
    running --> failed : tmux session missing unexpectedly
    running --> killed : Kill() called

    waiting_input --> killed : Kill() called
    waiting_input --> failed : tmux session missing

    complete --> [*]
    failed --> [*]
    killed --> [*]
```

State transitions trigger the `onStateChange` callback, which the router uses to send Signal notifications.

---

## Data Directory Layout

All runtime data lives under `~/.claude-signal/` (configurable via `data_dir`):

```
~/.claude-signal/
├── config.yaml          # Main configuration file
├── sessions.json        # Persistent session store
└── logs/
    ├── myhost-a3f2.log  # Output log for session a3f2
    ├── myhost-b7c1.log  # Output log for session b7c1
    └── ...
```

**sessions.json** is a JSON array of `Session` objects. It is updated on every state transition so the daemon can resume monitoring after a restart without losing session context.

**Log files** are written by tmux via `pipe-pane`. The monitor goroutine tails the log file, reading new lines as they appear.

---

## Configuration System

Configuration is loaded by `internal/config.Load()`, which:
1. Starts with `DefaultConfig()` (sensible defaults for all optional fields)
2. Reads and unmarshals the YAML file over the defaults
3. Re-applies defaults for any fields that yaml.Unmarshal left as zero values

This means the config file only needs to specify the fields that differ from defaults. The minimum viable config for `start` is:

```yaml
signal:
  account_number: +12125551234
  group_id: <base64-group-id>
```

---

## Extension Points

| Point | How to extend |
|---|---|
| Signal protocol | Implement `SignalBackend` interface |
| Command parser | Add cases to `router.Parse()` |
| Output detection | Add patterns to `monitorOutput()` in `session.Manager` |
| Persistent storage | Replace `session.Store` JSON with SQLite or similar |
| Multi-group support | Add group routing logic to `router.Router` |
