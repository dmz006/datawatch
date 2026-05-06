---
date: 2026-03-30
status: planned
---

# Plan: RTK (Rust Token Killer) Integration

## What is RTK?

[RTK](https://github.com/rtk-ai/rtk) is a Rust CLI proxy that intercepts AI coding agent
shell commands and compresses output before it reaches the LLM's context window, reducing
token consumption by 60-90%. It supports 100+ commands (git, npm, cargo, docker, kubectl, etc.)
and works via Claude Code's PreToolUse hook system.

## Integration Points with Datawatch

### 1. Token Savings Dashboard
**Effort:** Small (1-2 days)

RTK exposes `rtk gain --all --format json` with structured savings data.
Datawatch can poll this periodically and display per-session token efficiency metrics
in the System Statistics dashboard.

```
Session Stats Table:
  Session | Backend | State | Memory | Tokens Saved | RTK Active
  abc1    | claude  | running | 45MB | 12,340 saved | ✓
```

### 2. Session-Level Analytics
**Effort:** Small (1-2 days)

`rtk session` shows RTK adoption across recent sessions. Datawatch correlates
this with its own session tracking to flag sessions where RTK isn't active
or adoption is low.

### 3. Hook Coexistence
**Effort:** Medium (3-5 days)

Both datawatch MCP channel and RTK use Claude Code's hook system (PreToolUse).
They need to be chained carefully:
- RTK: rewrites Bash commands → compressed output
- Datawatch: captures session activity via MCP channel

**Solution:** Document the correct hook ordering. RTK's hook should fire first
(command rewrite), then datawatch's hook (session tracking). Both can coexist
in `.claude/settings.json`.

### 4. Tee Output Recovery
**Effort:** Small (1 day)

When RTK-proxied commands fail, full unfiltered output is saved to
`~/.local/share/rtk/tee/`. Datawatch can watch this directory and surface
failure details in the session timeline without re-running commands.

### 5. Discover Integration
**Effort:** Small (1-2 days)

`rtk discover --all --since N` identifies commands that could have been
compressed but weren't. Datawatch surfaces these as optimization
recommendations in the alerts or session detail view.

### 6. Config Management
**Effort:** Small (1 day)

RTK's `[hooks] exclude_commands` config (in `~/.config/rtk/config.toml`)
can be managed via datawatch's Settings UI. Certain commands may need raw
output for monitoring accuracy.

## Phases

### Phase 1: Detection & Display (1 week)
- Detect if RTK is installed (`rtk --version`)
- Show RTK status in Settings → About
- Display token savings in System Statistics dashboard
- `rtk gain --all --format json` polling every collection cycle

### Phase 2: Per-Session Analytics (1 week)
- Correlate `rtk session` with datawatch sessions
- Per-session token savings in session detail view
- Flag sessions without RTK active

### Phase 3: Hook Coexistence Documentation (2 days)
- Document correct hook ordering
- Test both hooks firing correctly
- Add setup wizard for RTK integration

## Prerequisites

- RTK installed: `cargo install rtk-cli` or download binary
- RTK initialized: `rtk init -g` (adds PreToolUse hook)
- Datawatch MCP channel configured for Claude sessions

## Configuration

```yaml
rtk:
  enabled: false           # enable RTK integration
  binary: rtk              # path to RTK binary
  show_savings: true       # display token savings in dashboard
  discover_interval: 300   # seconds between discover checks (0 = disabled)
```

## Estimated Effort

- Phase 1: 1 week
- Phase 2: 1 week
- Phase 3: 2 days
- Total: ~2.5 weeks

## Installation & Setup

### Recommended: `datawatch setup rtk`

The setup wizard should handle installation automatically:

```bash
datawatch setup rtk
```

Flow:
1. Check if `rtk` is in PATH → if yes, show version and skip install
2. If not: detect platform (Linux amd64/arm64, macOS, Windows)
3. Download latest release from GitHub API (`rtk-ai/rtk/releases/latest`)
4. Install to `~/.local/bin/rtk` (or user-chosen path)
5. Run `rtk init -g` to install the Claude Code PreToolUse hook
6. Save `rtk.enabled: true` and `rtk.binary: <path>` to config
7. Verify: `rtk --version`

**Communication channel setup:** NOT recommended — RTK requires filesystem
access and shell integration. Users should install via CLI. The messaging
`configure rtk.enabled=true` command should warn: "Install RTK first via CLI."

### Manual install (if user prefers)

```bash
# Cargo
cargo install rtk-cli

# Or download binary
curl -fsSL https://github.com/rtk-ai/rtk/releases/latest/download/rtk-linux-amd64 -o ~/.local/bin/rtk
chmod +x ~/.local/bin/rtk
rtk init -g
```

## Version Checking & Auto-Update

- `rtk.auto_update: true` in config — check for new RTK releases on the same
  schedule as datawatch updates
- Compare `rtk --version` output with GitHub latest release tag
- If update available: show notice in Settings → About and in `stats` output
- If `rtk.auto_update` enabled: download and replace binary automatically
- Include in `datawatch update` flow: update both datawatch and RTK

## LLM Support Matrix

RTK currently supports these LLM backends (from rtk-ai/rtk README):

| LLM Backend | RTK Support | Datawatch Backend |
|-------------|-------------|-------------------|
| Claude Code | ✅ Full (PreToolUse hook) | claude-code |
| Cursor | ✅ Full | N/A (IDE) |
| Copilot | ✅ Full | N/A (IDE) |
| Gemini CLI | ✅ Full | gemini |
| Aider | ⚠️ Partial | aider |
| Goose | ❓ Unknown | goose |
| opencode | ❓ Unknown | opencode |

**AGENT.md rule:** When adding a new LLM backend to datawatch, check if RTK
supports it. If yes, add RTK hook configuration to the backend's setup wizard.
If unknown, test compatibility and update the support matrix.

## Decision: Planned for future

RTK integration is planned but deferred because:
1. RTK is a new project — wait for API stability
2. Current priority is bug fixes and core stability
3. Integration is additive (no breaking changes needed)
4. Can be implemented incrementally (Phase 1 first)
5. User should install RTK separately until auto-install is tested
