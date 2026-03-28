# Plan: Backlog Bugs, Channel Self-Contained, Status Command

**Date:** 2026-03-28
**Version at planning:** v0.5.18
**Shipped in:** v0.5.19 (channel self-contained), v0.5.20 (bugs, status, ACP)

---

## Scope

Files affected:
- `cmd/datawatch/main.go`
- `internal/server/api.go`, `server.go`
- `internal/session/manager.go`
- `internal/llm/backends/ollama/backend.go`
- `internal/llm/backends/opencode/acpbackend.go`
- `internal/channel/` (new package)
- `internal/server/web/app.js`
- `AGENT.md`, `docs/claude-channel.md`

---

## Phases

### Phase 1 — Channel self-contained (v0.5.19) ✅ Done

| Step | Status |
|------|--------|
| Embed `channel/dist/index.js` in Go binary via `//go:embed` | Done |
| Auto-extract to `~/.datawatch/channel/channel.js` on start | Done |
| Auto-register with `claude mcp add --scope user` with env vars | Done |
| Add `/api/channel/ready` route (handler existed, route was missing) | Done |
| Remove broken `channelTaskSent` sync.Map + log-line detection | Done |
| Rebuild channel TypeScript; add `make channel-build` target | Done |
| Update `docs/claude-channel.md`: setup is now automatic | Done |

### Phase 2 — Agent rules and docs (v0.5.20) ✅ Done

| Step | Status |
|------|--------|
| AGENT.md: Planning Rules section | Done |
| `docs/claude-channel.md`: Node.js ≥18 prerequisite + install | Done |
| `setupChannelMCP()`: check node in PATH, warn if missing | Done |

### Phase 3 — Bugs (v0.5.20) ✅ Done

| Step | Status |
|------|--------|
| Ollama active status: HTTP `/api/tags` probe for remote host | Done |
| Session create: REST → navigate directly to new session detail | Done |
| `apiFetch()` helper in app.js | Done |
| `datawatch status` top-level command | Done |
| opencode ACP channel reply routing via `OnChannelReply` callback | Done |
| `session.Manager.SetOnSessionStart` callback | Done |
| `server.HTTPServer.BroadcastChannelReply` forwarding method | Done |

### Phase 4 — Remaining backlog items (not yet done)

| Item | Priority |
|------|----------|
| Android Chrome notification permission denied | P2 |
| Alerts: group by session + quick-reply command buttons | P2 |
| Web UI backend configure form | P2 |
| detect_prompt secondary action (notify/send_input) | P2 |
| signal-cli supervisor for auto-restart on crash | P2 |
| Ollama model dropdown during config | P3 |
| channel_enabled default + UI gating | P3 |
| Upgrade graceful reexec | P3 |

---

## Notes

- `channel/dist/` remains gitignored; the embed copy lives at `internal/channel/embed/channel.js`
- `make channel-build` syncs TS → embed after editing `channel/index.ts`
- ACP channel reply has a timing-safe pending map (`acpFullIDs`) so `SetACPFullID`
  can be called before the ACP server goroutine starts
- signal-go work deferred by user; plan at `/home/dmz/.claude/plans/bugs-and-signal-go.md`
