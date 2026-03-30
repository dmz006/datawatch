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

## Decision: Planned for future

RTK integration is planned but deferred because:
1. RTK is a new project — wait for API stability
2. Current priority is bug fixes and core stability
3. Integration is additive (no breaking changes needed)
4. Can be implemented incrementally (Phase 1 first)
