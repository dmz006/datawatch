# Data Flow Diagrams — datawatch

Detailed Mermaid diagrams showing how data moves through the system.
Each flow is documented in its own file under `docs/flow/`.

---

## Flow Diagram Index

| Diagram | File | Description |
|---------|------|-------------|
| [System Data Flow](flow/system-data-flow.md) | `flow/system-data-flow.md` | Top-level component interaction — Signal, daemon, tmux, PWA |
| [New Session Sequence](flow/new-session-flow.md) | `flow/new-session-flow.md` | Full sequence from `new:` command to session running in tmux |
| [Input Required Sequence](flow/input-required-flow.md) | `flow/input-required-flow.md` | Idle detection → prompt alert → user reply → resume |
| [Multi-Machine Sequence](flow/multi-machine-flow.md) | `flow/multi-machine-flow.md` | Two hosts sharing one Signal group — routing by hostname |
| [WebSocket Message Flow](flow/websocket-flow.md) | `flow/websocket-flow.md` | WS connect, subscribe, output push, input send, keepalive |
| [Signal JSON-RPC Flow](flow/signal-rpc-flow.md) | `flow/signal-rpc-flow.md` | stdin/stdout JSON-RPC protocol between datawatch and signal-cli |
| [Signal Message Flow](flow/signal-flow.md) | `flow/signal-flow.md` | Phone → Signal → signal-cli → router → session |
| [Multi-Source Message Flow](flow/multi-source-flow.md) | `flow/multi-source-flow.md` | Multiple messaging backends routing to session manager; includes proxy mode variant |
| [Persistence Flow](flow/persistence-flow.md) | `flow/persistence-flow.md` | When and why data is written to disk — config, sessions, logs |
| [DNS Channel Flow](flow/dns-channel-flow.md) | `flow/dns-channel-flow.md` | DNS TXT query encoding, HMAC auth, response fragmentation |
| [Proxy Mode Flow](flow/proxy-flow.md) | `flow/proxy-flow.md` | Remote server routing — session discovery, command forwarding, WS relay |

---

## Additional Architecture Diagrams

- [Architecture — Component Diagram + Proxy Mode](architecture.md) — includes connection, data, and WS relay diagrams
- [Application State Flow](app-flow.md) — daemon startup, CLI, command, monitoring flows
