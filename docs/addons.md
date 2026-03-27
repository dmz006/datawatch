# Writing Addons for datawatch

datawatch uses a registry pattern for both AI backends and messaging backends.
Adding a new backend requires implementing an interface and registering it.

## AI Backend Addons

Implement `llm.Backend` (see `internal/llm/backend.go`):

```go
type Backend interface {
    Name() string
    Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error
    SupportsInteractiveInput() bool
    Version() string
}
```

Place your backend in `internal/llm/backends/<name>/backend.go`.
Register via init():

```go
func init() { llm.Register(New()) }
```

Then import in `cmd/datawatch/main.go`:

```go
"github.com/dmz006/datawatch/internal/llm/backends/<name>"
```

(Use a blank import `_` if the package only registers via `init()`, or a named import
if you need to call its constructor to pass config values.)

## Messaging Backend Addons

Implement `messaging.Backend` (see `internal/messaging/backend.go`):

```go
type Backend interface {
    Name() string
    Send(recipient, message string) error
    Subscribe(ctx context.Context, handler func(Message)) error
    Link(deviceName string, onQR func(string)) error
    SelfID() string
    Close() error
}
```

Place in `internal/messaging/backends/<name>/backend.go`.
Instantiate and run in `cmd/datawatch/main.go`'s `runStart()`.

Add config fields in `internal/config/config.go` and wire in `runStart()`.

## Available Backends

### AI Backends

| Name | Package | Description |
|------|---------|-------------|
| claude-code | internal/llm/claudecode | Anthropic Claude Code CLI (default) |
| ollama | internal/llm/backends/ollama | Local models via Ollama |
| openwebui | internal/llm/backends/openwebui | OpenWebUI (OpenAI-compatible API) |

### Messaging Backends

| Name | Package | Description |
|------|---------|-------------|
| signal | internal/messaging/backends/signal | Signal via signal-cli |
| discord | internal/messaging/backends/discord | Discord bot |
| slack | internal/messaging/backends/slack | Slack RTM bot |
| web | internal/server | Built-in PWA/WebSocket |

## Configuration

Each backend has a corresponding section in `~/.datawatch/config.yaml`.
See [setup.md](setup.md) for full configuration reference.
