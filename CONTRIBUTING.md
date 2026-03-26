# Contributing to claude-signal

Thank you for your interest in contributing! claude-signal is a solo-maintainer project,
so patience is appreciated — reviews may take a few days.

## Reporting Bugs

1. Search [existing issues](https://github.com/dmz006/claude-signal/issues) first.
2. Open a new issue using the **Bug Report** template.
3. Include OS, versions, steps to reproduce, and relevant log output.

## Suggesting Features

1. Search existing issues for similar ideas.
2. Open a new issue using the **Feature Request** template.
3. Describe the problem you are solving, not just the desired solution.
4. For large changes, discuss before implementing — this saves everyone time.

## Development Setup

### Requirements

| Tool | Version | Purpose |
|---|---|---|
| Go | 1.22+ | Building claude-signal |
| signal-cli | 0.13+ | Signal protocol bridge |
| Java | 17+ | Required by signal-cli |
| tmux | Any recent | Session management |
| claude CLI | Latest | AI assistant (integration testing) |

### Clone and build

```bash
git clone https://github.com/dmz006/claude-signal
cd claude-signal
go build ./...
```

### Running locally

```bash
# Build and run with verbose output (signal-cli must already be linked)
go build -o bin/claude-signal ./cmd/claude-signal && ./bin/claude-signal start --verbose
```

## Project Structure

```
claude-signal/
├── cmd/claude-signal/     # Main entrypoint (cobra commands)
├── internal/
│   ├── config/            # Configuration loading and defaults
│   ├── llm/               # LLM backend interface + registry
│   │   └── claudecode/    # claude-code backend implementation
│   ├── messaging/         # Messaging backend interface
│   ├── router/            # Signal command parsing and routing
│   ├── server/            # HTTP/WebSocket server + PWA
│   │   └── web/           # Static PWA assets
│   ├── session/           # Session lifecycle management
│   └── signal/            # Signal protocol (signal-cli JSON-RPC)
├── docs/                  # Documentation
│   └── api/               # OpenAPI specification
└── install/               # Installation scripts
```

## Adding a New LLM Backend

Implement the `llm.Backend` interface from `internal/llm/backend.go`:

```go
type Backend interface {
    Name() string
    Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error
    SupportsInteractiveInput() bool
    Version() string
}
```

Steps:

1. Create `internal/llm/<backendname>/backend.go`.
2. Implement the interface.
3. Call `llm.Register(New(""))` in an `init()` function.
4. Import the package with a blank identifier in `cmd/claude-signal/main.go`:
   `import _ "github.com/dmz006/claude-signal/internal/llm/<backendname>"`
5. Write tests and update `docs/implementation.md`.

## Adding a New Messaging Backend

Implement the `messaging.Backend` interface from `internal/messaging/backend.go`:

```go
type Backend interface {
    Name() string
    Send(recipient, message string) error
    Subscribe(ctx context.Context, handler func(Message)) error
    Link(deviceName string, onQR func(qrURI string)) error
    SelfID() string
    Close() error
}
```

Steps:

1. Create `internal/messaging/<backendname>/backend.go`.
2. Implement the interface.
3. Wire it into the daemon startup logic.
4. Add configuration fields as needed.
5. Test, document, and submit a PR.

## Code Style

- Format with `gofmt -w .` before committing. No external linters are required for PRs.
- Add doc comments to all exported types and functions.
- Keep functions focused on a single responsibility.
- Prefer explicit error handling over panics.

## Commit Message Format

We use [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <short description>
```

Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`

Examples:
```
feat(llm): add aider backend implementation
fix(session): correctly detect tmux session exit on macOS
docs(api): add OpenAPI spec for /api/link endpoints
chore(deps): update gorilla/websocket to v1.5.3
```

## Pull Request Process

1. Fork the repository and branch off `main`:
   ```bash
   git checkout -b feat/my-feature
   ```
2. Make your changes. Add tests for new functionality.
3. Ensure code compiles: `go build ./...`
4. Format code: `gofmt -w .`
5. Update `CHANGELOG.md` under `[Unreleased]`.
6. Commit with a conventional commit message.
7. Push to your fork and open a PR using the PR template.
8. One maintainer review is required before merge.

All contributions are welcome. This is a solo-maintainer project — please be patient
with review turnaround.

## License

By contributing, you agree that your contributions will be licensed under the
[Polyform Noncommercial License 1.0.0](LICENSE).
