# Claude MCP channel mode flow

End-to-end picture of how `claude_channel_enabled: true` wires the
running claude-code session to the parent daemon for permission
relay, structured tool callbacks, and inbound notifications. Covers
the path that ran on the Node.js bridge prior to the native Go rewrite.

```
   ┌─── parent daemon startup ────────────────────────────────────────┐
   │                                                                  │
   │  if cfg.Session.ClaudeChannelEnabled:                            │
   │                                                                  │
   │    channel.Probe(<data_dir>)                                     │
   │      ├─→ NodePath(): node ≥ 18 in PATH (or DATAWATCH_NODE_BIN)   │
   │      ├─→ findNPM(): npm reachable                                │
   │      └─→ <data_dir>/channel/node_modules/@modelcontextprotocol   │
   │                                                                  │
   │      not ready ─→ [warn] startup line + "datawatch setup channel"│
   │                                                                  │
   │    channel.EnsureExtracted(<data_dir>)                           │
   │      • write embedded channel.js (//go:embed)                    │
   │      • write package.json                                        │
   │      • exec npm install --production                             │
   │                                                                  │
   │    channel.UnregisterGlobalMCP()  (drop legacy "datawatch")      │
   │    channel.CleanupStaleMCP(sessionExists)                        │
   │                                                                  │
   └────────────────────────────┬─────────────────────────────────────┘
                                │
                                ▼
   ┌─── per-session start (session.Manager onPreLaunch) ──────────────┐
   │                                                                  │
   │  channel.RegisterSessionMCP(sessionID, channelJSPath, env)       │
   │      → claude mcp add --scope user datawatch-<id>                │
   │            <node_path> <channel.js>                              │
   │            --env CLAUDE_SESSION_ID=<id>                          │
   │            --env DATAWATCH_PARENT_URL=…                          │
   │            --env DATAWATCH_BEARER_TOKEN=…                        │
   │            --env DATAWATCH_CHANNEL_PORT=0   (random)             │
   │                                                                  │
   │  claude itself launches inside tmux (always — channel mode is    │
   │  additive; console mode + tmux pane stay live)                   │
   │                                                                  │
   └────────────────────────────┬─────────────────────────────────────┘
                                │
                                ▼
   ┌─── channel.js subprocess (per session) ──────────────────────────┐
   │                                                                  │
   │  Job 1 — HTTP listener on 127.0.0.1:<random>/send                │
   │      daemon → channel.js notifications (PWA / Signal sends)      │
   │                                                                  │
   │  Job 2 — MCP `reply` tool                                        │
   │      claude calls reply(text) ──→ POST /api/channel/reply        │
   │                                  with bearer + session_id        │
   │                                                                  │
   │  Job 3 — Permission relay                                        │
   │      claude tool-approval prompt arrives                         │
   │          ─→ POST /api/channel/permission   (sync, blocks)        │
   │              ─→ daemon dispatches to operator                    │
   │                  via Signal / Telegram / PWA toast               │
   │              ─→ operator answers (allow / deny / always)         │
   │              ─→ daemon → channel.js → claude                     │
   │                                                                  │
   │      hard timeout: 5 min (configurable in the Go bridge)         │
   │                                                                  │
   └──────────────────────────────────────────────────────────────────┘

   Console mode (tmux) keeps running in parallel for everything that
   the channel cannot carry: folder-trust prompts, `claude auth login`,
   subprocess password prompts, vim/etc. See docs/claude-channel.md
   for the per-feature mode matrix.
```

## State signals

| Signal | Meaning |
|---|---|
| `channel_ready: true` on the session | channel.js handshake completed; PWA suppresses console-based prompt detection |
| `channel_ready: false` | falling back to console; tmux is the sole authority |

## Failure modes

| Symptom | Likely cause | Fix |
|---|---|---|
| Startup `[warn] channel runtime not ready` | node / npm missing | `datawatch setup channel`, or disable `claude_channel_enabled` |
| Session never reaches `channel_ready: true` | `claude mcp list` empty for `datawatch-<id>` | check `claude mcp` output; check daemon `[channel]` log lines |
| Permission prompt never arrives at operator | comm backend not configured | check Signal / Telegram backend status |
| Replies show in tmux but not in chat history | bearer token mismatch | re-register MCP (restart session) |

## Related

- Operator doc: [`docs/claude-channel.md`](../claude-channel.md)
- Probe + extract: `internal/channel/channel.go` → `Probe()`, `EnsureExtracted()`
- Bridge source: `internal/channel/embed/channel.js`
- Handlers: `internal/server/api.go` → `handleChannelReply`, `handleChannelPermission`
- Native Go replacement design: [`docs/plans/2026-04-25-bl174-go-mcp-channel-and-slim-container.md`](../plans/2026-04-25-bl174-go-mcp-channel-and-slim-container.md)
