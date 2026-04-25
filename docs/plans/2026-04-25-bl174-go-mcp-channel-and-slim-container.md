# BL174 — Native Go MCP channel server + slim claude container

**Status:** Design — not yet implemented
**Filed:** 2026-04-25
**Predecessor:** v4.2.0 B39 (short-term: probe + `datawatch setup channel` + docs)
**Target release:** v4.3.0 (separate dedicated patch; not part of v4.2.x train)

## Problem

Two related operator surprises that the v4.2.0 short-term fix only papered
over:

1. **`datawatch` claims to be a self-contained binary**, but enabling
   `claude_channel_enabled: true` extracts `internal/channel/embed/channel.js`
   and shells out to `npm install @modelcontextprotocol/sdk` on first session
   start. The `setup channel` cmd makes this explicit but doesn't remove the
   dependency.
2. **The agent-claude container doesn't ship node** (claude.exe is a native
   ELF binary), so channel mode does not work inside the container. Either
   we add node (regressing the size win — the whole reason claude.exe is
   copied bare out of a builder stage) or the container can never use the
   channel/MCP path. Today it silently falls back to console-only.

## Proposed approach

### Part 1 — Native Go MCP server

Replace `internal/channel/embed/channel.js` with a Go process linked
against `github.com/mark3labs/mcp-go` (same SDK already in use for the
parent daemon's MCP server in `internal/mcp/server.go`). Build it as a
**second binary** under `cmd/datawatch-channel/` so the parent daemon
can stay lean and the channel binary can be deployed independently.

Why a second binary, not in-process:
- claude registers MCP servers via `claude mcp add <name> <command>
  <args…>`. The command spawns one process per session. In-process
  would require a long-lived daemon mode + per-session subprocess
  shims — more complex than just shipping a small Go binary.
- A second binary keeps the surface tiny (~10 MB statically linked
  vs the ~50 MB parent daemon).
- Operators on systems where the parent isn't available (e.g. CI
  agent containers) can still use the channel by dropping in just
  the `datawatch-channel` binary.

Surface preserved 1:1 with the existing `channel.js`:
- HTTP listener on `:7433/send` (or `$DATAWATCH_CHANNEL_PORT=0`
  random) for daemon→Claude notifications.
- `reply` MCP tool for Claude→daemon via `POST /api/channel/reply`.
- Permission relay: forwards Claude's tool-approval requests to the
  daemon for user prompting via Signal/Telegram/etc.
- Same env contract: `CLAUDE_SESSION_ID`, `DATAWATCH_PARENT_URL`,
  `DATAWATCH_BEARER_TOKEN`, `DATAWATCH_CHANNEL_PORT`.

Embed `cmd/datawatch-channel/datawatch-channel` into the parent
daemon binary via `//go:embed` (same pattern as `channel.js` today)
so `EnsureExtracted` writes a binary instead of a JS file. The
parent's `RegisterSessionMCP` flips from `claude mcp add … node
<channelJSPath>` to `claude mcp add … <channelBinPath>`.

### Part 2 — Slim claude container

The current agent-claude already does the right thing for claude.exe.
What's missing is a deterministic story for the channel:

1. **Embed the Go channel binary** in the parent datawatch binary
   (Part 1 dependency). Container's `/usr/local/bin/datawatch`
   already has it — `setup channel` extracts.
2. **Drop the install of `nodejs`** anywhere in agent-claude or
   agent-base (currently neither installs it, but a regression
   would silently re-introduce the dep).
3. **Container size audit pass** — measure agent-claude before/after
   and document the win in the release notes. Expected: no change
   to claude.exe layer; channel layer drops from `node binary
   (~70 MB) + node_modules (~50 MB) + channel.js` to a single
   ~10 MB Go binary, but only when channel is enabled.

Stretch: explore distroless or alpine for agent-base. Out of scope
for v4.3.0; track separately if the size pass shows >100 MB
remaining headroom in the bookworm base.

## Migration

- v4.2.0 (shipped) — Node.js path is the only path. `setup channel`
  + warn + docs.
- v4.3.0 (this BL) — Go binary becomes the default. Old extracted
  `~/.datawatch/channel/channel.js` + `node_modules/` directory is
  detected on startup and removed; replaced with the Go binary.
- v4.4.0 — `channel.js` source removed from `internal/channel/embed/`.
  The migration removal code stays for one more release as a safety
  net.

## Open questions

1. **Single binary vs subdirectory binary**: ship as
   `datawatch channel server` subcommand of the main daemon binary
   (no second binary), with claude `mcp add` pointing at
   `datawatch channel server`? Pro: one fewer artifact to track.
   Con: per-session spawn loads the entire 50 MB daemon process for
   what should be a small bridge.
2. **MCP protocol version**: SDK is on a faster cadence than the
   daemon; pin the wire version explicitly in the bridge so claude
   updates don't break us silently.
3. **Permission relay backpressure**: today's JS bridge pushes
   permission requests to the daemon synchronously and blocks on
   the operator response. With Go we can implement context-aware
   timeouts properly (today: hard-coded 5 min). Surface as
   `channel.permission_timeout_seconds`.
4. **Container regression test**: add a minimal compose-up test
   that boots agent-claude, starts a session in channel mode, and
   asserts the bridge connects without npm activity.

## Tasks (sprint plan)

| # | Task | Owner | Notes |
|---|------|-------|-------|
| 1 | `cmd/datawatch-channel/` skeleton: HTTP listener, `reply` MCP tool, permission-relay POST | | match `channel.js` byte-for-byte semantics; cross-build all 6 platforms |
| 2 | Reusable HTTP client helper for the parent's `/api/channel/reply` and `/api/channel/permission` (`internal/channelclient/`) | | shared with the parent daemon |
| 3 | `//go:embed` the channel binary into the parent; rework `EnsureExtracted` to write a binary + chmod 755 | | swap `channel.js` for binary; keep `Probe()` semantics — now it just checks the extracted binary exists |
| 4 | `RegisterSessionMCP` switch: drop `nodePath`; pass the channel binary path directly | | `claude mcp add` arglist trims `node` |
| 5 | Migration: detect old `~/.datawatch/channel/channel.js` + `node_modules/` and remove | | log a `[migrate]` line; idempotent |
| 6 | Tests: bridge unit tests (HTTP listener + MCP handler); integration test parametrised over old/new bridges so the swap can land first then `channel.js` removal lands second | | run inside CI with a stub claude exec |
| 7 | Container size audit: build agent-claude before/after, measure, write up | | release notes blurb |
| 8 | Stretch: distroless / alpine spike for agent-base | | size win must justify the operator-debugging penalty |
| 9 | Docs: update `docs/claude-channel.md` "Runtime requirements" → "no runtime requirements"; remove BL174 backlog entry; add v4.3.0 release notes | | |

Estimate: 4–5 days for parts 1–6 + tests; 1 day for the container
audit + docs. Stretch (distroless) deferred unless audit shows clear
upside.

## Acceptance criteria

- [ ] `datawatch start --foreground` boots cleanly with channel mode
      enabled on a host with no Node.js installed and no
      `~/.datawatch/channel/` directory.
- [ ] `claude mcp list` shows a registered `datawatch-<id>` server
      on session start; the registered command is the Go binary, not
      `node`.
- [ ] Existing operator workflows (Signal permission relay, reply
      tool) work unchanged.
- [ ] agent-claude container size is documented before/after; channel
      mode functions inside the container without node.
- [ ] `internal/channel/probe_test.go` updated; old js-specific
      assertions removed.
- [ ] CHANGELOG entry under v4.3.0; backlog README closes BL174.
